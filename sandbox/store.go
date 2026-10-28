package main

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alkiranet/alkira-client-go/alkira"
)

const defaultBillingAccountNumber = "5-SANDBOX01"

var banPattern = regexp.MustCompile(`^[0-9]-[A-Z0-9]{6,10}$`)

// apiError is returned to the provider as a JSON body with the given status.
type apiError struct {
	Status  int          `json:"statusCode"`
	Message string       `json:"message"`
	Errors  []fieldError `json:"errors,omitempty"`
}

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	if len(e.Errors) == 0 {
		return e.Message
	}
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + ": " + f.Message
	}
	return e.Message + ": " + strings.Join(parts, "; ")
}

func badRequest(errs []fieldError) error {
	if len(errs) == 0 {
		return nil
	}
	return &apiError{Status: 400, Message: "validation failed", Errors: errs}
}

func notFound(kind string, id int) error {
	return &apiError{Status: 404, Message: fmt.Sprintf("%s %d not found", kind, id)}
}

func conflict(format string, args ...any) error {
	return &apiError{Status: 400, Message: fmt.Sprintf(format, args...)}
}

type event struct {
	Time    time.Time `json:"time"`
	Method  string    `json:"method"`
	Path    string    `json:"path"`
	Status  int       `json:"status"`
	Message string    `json:"message,omitempty"`
}

// Store is the sandbox's in-memory tenant network. All access goes through mu.
type Store struct {
	mu             sync.Mutex
	nextID         int
	provisionDelay time.Duration
	seedExternal   bool
	gatewayReadyAt map[int]time.Time
	gateways       *collection[alkira.MultiCloudGateway]
	connectors     *collection[alkira.ConnectorMultiCloudGateway]
	segments       *collection[alkira.Segment]
	groups         *collection[alkira.Group]
	tags           *collection[alkira.BillingTag]
	events         []event
	updated        time.Time
}

func NewStore(provisionDelay time.Duration, seedExternal bool) *Store {
	s := &Store{provisionDelay: provisionDelay, seedExternal: seedExternal}
	s.reset()
	return s
}

func (s *Store) reset() {
	s.nextID = 1
	s.gatewayReadyAt = map[int]time.Time{}
	s.events = nil
	s.updated = time.Now()
	s.gateways = &collection[alkira.MultiCloudGateway]{
		kind: "multi-cloud gateway", items: map[int]*alkira.MultiCloudGateway{},
		name:     func(t *alkira.MultiCloudGateway) string { return t.Name },
		setID:    func(t *alkira.MultiCloudGateway, id int) { t.Id = num(id) },
		validate: s.validateGateway, finalize: s.finalizeGateway, beforeDelete: s.beforeDeleteGateway,
	}
	s.connectors = &collection[alkira.ConnectorMultiCloudGateway]{
		kind: "multi-cloud gateway connector", items: map[int]*alkira.ConnectorMultiCloudGateway{},
		name:     func(t *alkira.ConnectorMultiCloudGateway) string { return t.Name },
		setID:    func(t *alkira.ConnectorMultiCloudGateway, id int) { t.Id = num(id) },
		validate: s.validateConnector, finalize: s.finalizeConnector,
	}
	s.segments = &collection[alkira.Segment]{
		kind: "segment", items: map[int]*alkira.Segment{},
		name:     func(t *alkira.Segment) string { return t.Name },
		setID:    func(t *alkira.Segment, id int) { t.Id = num(id) },
		validate: s.validateSegment, beforeDelete: s.beforeDeleteSegment,
	}
	s.groups = &collection[alkira.Group]{
		kind: "group", items: map[int]*alkira.Group{},
		name:     func(t *alkira.Group) string { return t.Name },
		setID:    func(t *alkira.Group, id int) { t.Id = num(id) },
		validate: s.validateGroup, beforeDelete: s.beforeDeleteGroup,
	}
	s.tags = &collection[alkira.BillingTag]{
		kind: "billing tag", items: map[int]*alkira.BillingTag{},
		name:     func(t *alkira.BillingTag) string { return t.Name },
		setID:    func(t *alkira.BillingTag, id int) { t.Id = num(id) },
		validate: s.validateTag, beforeDelete: s.beforeDeleteTag,
	}
	if s.seedExternal {
		s.seedExternalGateway()
	}
}

// seedExternalGateway adds a gateway that was created in Lumen Connect rather than
// through this API, for import, data source and read-only scenarios.
func (s *Store) seedExternalGateway() {
	id := s.allocID()
	s.gateways.items[id] = &alkira.MultiCloudGateway{
		Id: num(id), Name: "lumen-connect-native-gw", Tier: "50GBPS",
		Description:          "Created in Lumen Connect",
		BillingAccountNumber: defaultBillingAccountNumber,
		NativeId:             fmt.Sprintf("lmcgw-%08x", uint32(id)*2654435761),
		State:                "PROVISIONED", External: true,
	}
}

func num(id int) json.Number { return json.Number(fmt.Sprint(id)) }

func (s *Store) allocID() int {
	id := s.nextID
	s.nextID++
	return id
}

// tick advances time-based state: gateways leave PROVISIONING once their delay passes.
func (s *Store) tick() {
	now := time.Now()
	for id, at := range s.gatewayReadyAt {
		if now.After(at) {
			if g, ok := s.gateways.items[id]; ok && g.State == "PROVISIONING" {
				g.State = "PROVISIONED"
				s.updated = now
			}
			delete(s.gatewayReadyAt, id)
		}
	}
}

func (s *Store) record(e event) {
	s.events = append(s.events, e)
	if len(s.events) > 60 {
		s.events = s.events[len(s.events)-60:]
	}
}

// collection is one REST resource type backed by a map keyed by ID.
type collection[T any] struct {
	kind         string
	items        map[int]*T
	name         func(*T) string
	setID        func(*T, int)
	validate     func(t *T, id int, old *T) []fieldError
	finalize     func(t *T, id int, old *T)
	beforeDelete func(id int) error
	afterDelete  func(id int)
}

func (c *collection[T]) ids() []int {
	ids := make([]int, 0, len(c.items))
	for id := range c.items {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (c *collection[T]) list(name string) []*T {
	out := []*T{}
	for _, id := range c.ids() {
		if name == "" || c.name(c.items[id]) == name {
			out = append(out, c.items[id])
		}
	}
	return out
}

func (c *collection[T]) byName(name string) (int, *T) {
	for _, id := range c.ids() {
		if c.name(c.items[id]) == name {
			return id, c.items[id]
		}
	}
	return 0, nil
}

func (c *collection[T]) get(id int) (*T, error) {
	t, ok := c.items[id]
	if !ok {
		return nil, notFound(c.kind, id)
	}
	return t, nil
}

func (c *collection[T]) decode(body []byte) (*T, error) {
	var t T
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, &apiError{Status: 400, Message: "malformed " + c.kind + " request: " + err.Error()}
	}
	return &t, nil
}

func (c *collection[T]) uniqueName(t *T, id int) []fieldError {
	n := c.name(t)
	if strings.TrimSpace(n) == "" {
		return []fieldError{{"name", "must not be empty"}}
	}
	if len(n) > 64 {
		return []fieldError{{"name", "must be at most 64 characters"}}
	}
	if otherID, _ := c.byName(n); otherID != 0 && otherID != id {
		return []fieldError{{"name", fmt.Sprintf("a %s named %q already exists (id %d)", c.kind, n, otherID)}}
	}
	return nil
}

func (c *collection[T]) create(s *Store, body []byte) (*T, error) {
	t, err := c.decode(body)
	if err != nil {
		return nil, err
	}
	errs := c.uniqueName(t, 0)
	if c.validate != nil {
		errs = append(errs, c.validate(t, 0, nil)...)
	}
	if err := badRequest(errs); err != nil {
		return nil, err
	}
	id := s.allocID()
	c.setID(t, id)
	if c.finalize != nil {
		c.finalize(t, id, nil)
	}
	c.items[id] = t
	return t, nil
}

func (c *collection[T]) update(id int, body []byte) (*T, error) {
	old, err := c.get(id)
	if err != nil {
		return nil, err
	}
	t, err := c.decode(body)
	if err != nil {
		return nil, err
	}
	errs := c.uniqueName(t, id)
	if c.validate != nil {
		errs = append(errs, c.validate(t, id, old)...)
	}
	if err := badRequest(errs); err != nil {
		return nil, err
	}
	c.setID(t, id)
	if c.finalize != nil {
		c.finalize(t, id, old)
	}
	c.items[id] = t
	return t, nil
}

func (c *collection[T]) remove(id int) error {
	if _, err := c.get(id); err != nil {
		return err
	}
	if c.beforeDelete != nil {
		if err := c.beforeDelete(id); err != nil {
			return err
		}
	}
	delete(c.items, id)
	return nil
}

// --- multi-cloud gateways ---

func (s *Store) validateGateway(g *alkira.MultiCloudGateway, id int, old *alkira.MultiCloudGateway) []fieldError {
	var errs []fieldError
	if !contains(mcgwTiers, g.Tier) {
		errs = append(errs, fieldError{"tier", fmt.Sprintf("%q is not a valid tier; expected one of %s", g.Tier, strings.Join(mcgwTiers, ", "))})
	}
	if g.BillingAccountNumber != "" && !banPattern.MatchString(g.BillingAccountNumber) {
		errs = append(errs, fieldError{"billingAccountNumber", fmt.Sprintf("%q is not a valid Lumen billing account number (expected e.g. 5-ABC12345)", g.BillingAccountNumber)})
	}
	if old != nil {
		if g.BillingAccountNumber != "" && g.BillingAccountNumber != old.BillingAccountNumber {
			errs = append(errs, fieldError{"billingAccountNumber", "cannot be changed on an existing gateway; replace the gateway instead"})
		}
		if old.External {
			errs = append(errs, fieldError{"external", "gateways created in Lumen Connect are read-only here"})
		}
	}
	return errs
}

func (s *Store) finalizeGateway(g *alkira.MultiCloudGateway, id int, old *alkira.MultiCloudGateway) {
	g.NativeId = fmt.Sprintf("lmcgw-%08x", uint32(id)*2654435761)
	if old == nil {
		if g.BillingAccountNumber == "" {
			g.BillingAccountNumber = defaultBillingAccountNumber
		}
		g.State = "PROVISIONING"
		s.gatewayReadyAt[id] = time.Now().Add(s.provisionDelay)
		return
	}
	if g.BillingAccountNumber == "" {
		g.BillingAccountNumber = old.BillingAccountNumber
	}
	g.State, g.External = old.State, old.External
	g.ConnectorIds = old.ConnectorIds
}

func (s *Store) beforeDeleteGateway(id int) error {
	g := s.gateways.items[id]
	if len(g.ConnectorIds) > 0 {
		return conflict("multi-cloud gateway %d still has connectors %v attached; delete them first", id, g.ConnectorIds)
	}
	if g.External {
		return conflict("multi-cloud gateway %d was created in Lumen Connect and cannot be deleted here", id)
	}
	delete(s.gatewayReadyAt, id)
	return nil
}

func (s *Store) syncGatewayConnectors() {
	for _, g := range s.gateways.items {
		g.ConnectorIds = nil
	}
	for _, cid := range s.connectors.ids() {
		c := s.connectors.items[cid]
		if g, ok := s.gateways.items[c.MultiCloudGatewayId]; ok {
			g.ConnectorIds = append(g.ConnectorIds, cid)
		}
	}
}

// --- multi-cloud gateway connectors ---

func (s *Store) validateConnector(c *alkira.ConnectorMultiCloudGateway, id int, old *alkira.ConnectorMultiCloudGateway) []fieldError {
	var errs []fieldError
	add := func(f, format string, args ...any) { errs = append(errs, fieldError{f, fmt.Sprintf(format, args...)}) }

	if !contains(cxps, c.Cxp) {
		add("cxp", "%q is not a CXP of this tenant network; expected one of %s", c.Cxp, strings.Join(cxps, ", "))
	}
	if _, ok := connectorSizeMbps[c.Size]; !ok {
		add("size", "%q is not a valid connector size", c.Size)
	}
	if g, ok := s.gateways.items[c.MultiCloudGatewayId]; !ok {
		add("multiCloudGatewayId", "multi-cloud gateway %d does not exist", c.MultiCloudGatewayId)
	} else if g.State != "PROVISIONED" {
		add("multiCloudGatewayId", "multi-cloud gateway %d is %s; connectors can only attach to a PROVISIONED gateway", c.MultiCloudGatewayId, g.State)
	}
	if old != nil && old.MultiCloudGatewayId != c.MultiCloudGatewayId {
		add("multiCloudGatewayId", "cannot be changed on an existing connector")
	}
	if c.Group != "" {
		if gid, _ := s.groups.byName(c.Group); gid == 0 {
			add("group", "group %q does not exist", c.Group)
		}
	}
	for _, t := range c.BillingTags {
		if _, ok := s.tags.items[t]; !ok {
			add("billingTags", "billing tag %d does not exist", t)
		}
	}

	_, prefix, err := net.ParseCIDR(c.LoopbackPrefix)
	switch {
	case err != nil || prefix.IP.To4() == nil || prefix.String() != c.LoopbackPrefix:
		add("loopbackPrefix", "%q is not a valid IPv4 network address", c.LoopbackPrefix)
	case !strings.HasSuffix(c.LoopbackPrefix, "/26"):
		add("loopbackPrefix", "%q must be a /26", c.LoopbackPrefix)
	default:
		for _, oid := range s.connectors.ids() {
			o := s.connectors.items[oid]
			if oid == id || o.MultiCloudGatewayId != c.MultiCloudGatewayId {
				continue
			}
			if _, op, err := net.ParseCIDR(o.LoopbackPrefix); err == nil && (op.Contains(prefix.IP) || prefix.Contains(op.IP)) {
				add("loopbackPrefix", "%s overlaps connector %d's loopback prefix %s on the same gateway", c.LoopbackPrefix, oid, o.LoopbackPrefix)
			}
		}
	}

	if len(c.SegmentOptions) != 1 {
		add("segmentOptions", "exactly one segment option is required, got %d", len(c.SegmentOptions))
	}
	for i, so := range c.SegmentOptions {
		if sid, _ := s.segments.byName(so.SegmentName); sid == 0 {
			add(fmt.Sprintf("segmentOptions[%d].segmentName", i), "segment %q does not exist", so.SegmentName)
		}
	}

	if len(c.Instances) == 0 {
		add("instances", "at least one instance is required")
	}
	if len(c.Instances) > 4 {
		add("instances", "at most 4 instances are allowed, got %d", len(c.Instances))
	}
	names, regions := map[string]int{}, map[string]int{}
	oldIDs := map[int]bool{}
	if old != nil {
		for _, in := range old.Instances {
			oldIDs[in.Id] = true
		}
	}
	for i, in := range c.Instances {
		f := fmt.Sprintf("instances[%d]", i)
		if strings.TrimSpace(in.Name) == "" {
			add(f+".name", "must not be empty")
		} else if p, dup := names[in.Name]; dup {
			add(f+".name", "duplicates instances[%d].name %q", p, in.Name)
		} else {
			names[in.Name] = i
		}
		if !isOnRampRegion(in.OnRampRegion) {
			add(f+".onRampRegion", "%q is not a supported Lumen on-ramp region (GET /sandbox/on-ramp-regions lists them)", in.OnRampRegion)
		} else if p, dup := regions[in.OnRampRegion]; dup {
			add(f+".onRampRegion", "duplicates instances[%d].onRampRegion %q", p, in.OnRampRegion)
		} else {
			regions[in.OnRampRegion] = i
		}
		if in.Id != 0 && !oldIDs[in.Id] {
			add(f+".id", "instance %d does not belong to this connector", in.Id)
		}
	}
	return errs
}

func (s *Store) finalizeConnector(c *alkira.ConnectorMultiCloudGateway, id int, old *alkira.ConnectorMultiCloudGateway) {
	c.ConfigStatus = "ACTIVE"
	c.ImplicitGroupId = 9000 + id
	prev := map[int]alkira.ConnectorMultiCloudGatewayInstance{}
	if old != nil {
		for _, in := range old.Instances {
			prev[in.Id] = in
		}
	}
	for i := range c.Instances {
		in := &c.Instances[i]
		if in.Id == 0 {
			in.Id = s.allocID()
		}
		in.BandwidthInMbps = connectorSizeMbps[c.Size]
		if p, ok := prev[in.Id]; ok && p.LumenConnectionId != "" {
			in.LumenConnectionId = p.LumenConnectionId
		} else {
			in.LumenConnectionId = fmt.Sprintf("LC-%06d", 100000+in.Id*37)
		}
	}
}

// --- segments, groups, billing tags ---

func (s *Store) validateSegment(seg *alkira.Segment, id int, old *alkira.Segment) []fieldError {
	var errs []fieldError
	if seg.Asn < 1 || seg.Asn > 4294967294 {
		errs = append(errs, fieldError{"asn", fmt.Sprintf("%d is not a valid ASN", seg.Asn)})
	}
	if len(seg.IpBlocks.Values) == 0 {
		errs = append(errs, fieldError{"ipBlocks", "at least one CIDR is required"})
	}
	for i, c := range seg.IpBlocks.Values {
		if _, _, err := net.ParseCIDR(c); err != nil {
			errs = append(errs, fieldError{fmt.Sprintf("ipBlocks.values[%d]", i), fmt.Sprintf("%q is not a valid CIDR", c)})
		}
	}
	return errs
}

func (s *Store) segmentInUse(name string) []int {
	var users []int
	for _, cid := range s.connectors.ids() {
		for _, so := range s.connectors.items[cid].SegmentOptions {
			if so.SegmentName == name {
				users = append(users, cid)
			}
		}
	}
	return users
}

func (s *Store) beforeDeleteSegment(id int) error {
	if users := s.segmentInUse(s.segments.items[id].Name); len(users) > 0 {
		return conflict("segment %d is used by connectors %v", id, users)
	}
	return nil
}

func (s *Store) validateGroup(*alkira.Group, int, *alkira.Group) []fieldError { return nil }

func (s *Store) beforeDeleteGroup(id int) error {
	name := s.groups.items[id].Name
	for _, cid := range s.connectors.ids() {
		if s.connectors.items[cid].Group == name {
			return conflict("group %d is used by connector %d", id, cid)
		}
	}
	return nil
}

func (s *Store) validateTag(*alkira.BillingTag, int, *alkira.BillingTag) []fieldError { return nil }

func (s *Store) beforeDeleteTag(id int) error {
	for _, cid := range s.connectors.ids() {
		for _, t := range s.connectors.items[cid].BillingTags {
			if t == id {
				return conflict("billing tag %d is used by connector %d", id, cid)
			}
		}
	}
	return nil
}
