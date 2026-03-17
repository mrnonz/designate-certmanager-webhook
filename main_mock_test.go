package main

import (
	"fmt"
	"testing"

	"github.com/gophercloud/gophercloud/openstack/dns/v2/recordsets"
	"github.com/gophercloud/gophercloud/openstack/dns/v2/zones"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
)

// mockDNSClient implements dnsClient for unit testing.
type mockDNSClient struct {
	zones      []zones.Zone
	recordSets map[string][]recordsets.RecordSet // keyed by zoneID

	createCalls int
	updateCalls int
	deleteCalls int
}

func newMockDNSClient() *mockDNSClient {
	return &mockDNSClient{
		recordSets: make(map[string][]recordsets.RecordSet),
	}
}

func (m *mockDNSClient) ListZones(opts zones.ListOptsBuilder) ([]zones.Zone, error) {
	if lo, ok := opts.(zones.ListOpts); ok {
		var filtered []zones.Zone
		for _, z := range m.zones {
			if lo.Name == "" || z.Name == lo.Name {
				filtered = append(filtered, z)
			}
		}
		return filtered, nil
	}
	return m.zones, nil
}

func (m *mockDNSClient) ListRecordSetsByZone(zoneID string, opts recordsets.ListOptsBuilder) ([]recordsets.RecordSet, error) {
	rrs := m.recordSets[zoneID]
	if lo, ok := opts.(recordsets.ListOpts); ok {
		var filtered []recordsets.RecordSet
		for _, rr := range rrs {
			if (lo.Name == "" || rr.Name == lo.Name) &&
				(lo.Type == "" || rr.Type == lo.Type) {
				filtered = append(filtered, rr)
			}
		}
		return filtered, nil
	}
	return rrs, nil
}

func (m *mockDNSClient) CreateRecordSet(zoneID string, opts recordsets.CreateOptsBuilder) (*recordsets.RecordSet, error) {
	m.createCalls++
	co, ok := opts.(recordsets.CreateOpts)
	if !ok {
		return nil, fmt.Errorf("unexpected opts type %T", opts)
	}
	rs := recordsets.RecordSet{
		ID:      fmt.Sprintf("rs-%s-%d", zoneID, len(m.recordSets[zoneID])),
		ZoneID:  zoneID,
		Name:    co.Name,
		Type:    co.Type,
		Records: co.Records,
	}
	m.recordSets[zoneID] = append(m.recordSets[zoneID], rs)
	return &rs, nil
}

func (m *mockDNSClient) UpdateRecordSet(zoneID string, rrsetID string, opts recordsets.UpdateOptsBuilder) (*recordsets.RecordSet, error) {
	m.updateCalls++
	uo, ok := opts.(recordsets.UpdateOpts)
	if !ok {
		return nil, fmt.Errorf("unexpected opts type %T", opts)
	}
	for i, rr := range m.recordSets[zoneID] {
		if rr.ID == rrsetID {
			m.recordSets[zoneID][i].Records = uo.Records
			updated := m.recordSets[zoneID][i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("recordset %s not found in zone %s", rrsetID, zoneID)
}

func (m *mockDNSClient) DeleteRecordSet(zoneID string, rrsetID string) error {
	m.deleteCalls++
	rrs := m.recordSets[zoneID]
	for i, rr := range rrs {
		if rr.ID == rrsetID {
			m.recordSets[zoneID] = append(rrs[:i], rrs[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("recordset %s not found in zone %s", rrsetID, zoneID)
}

func newChallengeRequest(zone, fqdn, key string) *v1alpha1.ChallengeRequest {
	return &v1alpha1.ChallengeRequest{
		ResolvedZone: zone,
		ResolvedFQDN: fqdn,
		Key:          key,
	}
}

const (
	testZoneID   = "zone-1"
	testZoneName = "example.com."
	testFQDN     = "_acme-challenge.example.com."
)

func solverWithMock(mock *mockDNSClient) *designateDNSProviderSolver {
	return &designateDNSProviderSolver{client: mock}
}

func setupMockWithZone(mock *mockDNSClient) {
	mock.zones = []zones.Zone{{ID: testZoneID, Name: testZoneName}}
}

// --- Present tests ---

func TestPresent_CreatesNewRecordSet(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	solver := solverWithMock(mock)

	ch := newChallengeRequest(testZoneName, testFQDN, "challenge-token-1")
	err := solver.Present(ch)

	require.NoError(t, err)
	assert.Equal(t, 1, mock.createCalls)
	assert.Equal(t, 0, mock.updateCalls)

	rrs := mock.recordSets[testZoneID]
	require.Len(t, rrs, 1)
	assert.Equal(t, testFQDN, rrs[0].Name)
	assert.Equal(t, "TXT", rrs[0].Type)
	assert.Equal(t, []string{`"challenge-token-1"`}, rrs[0].Records)
}

func TestPresent_AppendsToExistingRecordSet(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	mock.recordSets[testZoneID] = []recordsets.RecordSet{
		{
			ID:      "existing-rs",
			ZoneID:  testZoneID,
			Name:    testFQDN,
			Type:    "TXT",
			Records: []string{`"old-token"`},
		},
	}
	solver := solverWithMock(mock)

	ch := newChallengeRequest(testZoneName, testFQDN, "new-token")
	err := solver.Present(ch)

	require.NoError(t, err)
	assert.Equal(t, 0, mock.createCalls)
	assert.Equal(t, 1, mock.updateCalls)

	rrs := mock.recordSets[testZoneID]
	require.Len(t, rrs, 1)
	assert.Equal(t, []string{`"old-token"`, `"new-token"`}, rrs[0].Records)
}

func TestPresent_SkipsDuplicateRecord(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	mock.recordSets[testZoneID] = []recordsets.RecordSet{
		{
			ID:      "existing-rs",
			ZoneID:  testZoneID,
			Name:    testFQDN,
			Type:    "TXT",
			Records: []string{`"already-exists"`},
		},
	}
	solver := solverWithMock(mock)

	ch := newChallengeRequest(testZoneName, testFQDN, "already-exists")
	err := solver.Present(ch)

	require.NoError(t, err)
	assert.Equal(t, 0, mock.createCalls)
	assert.Equal(t, 0, mock.updateCalls)
	assert.Equal(t, []string{`"already-exists"`}, mock.recordSets[testZoneID][0].Records)
}

func TestPresent_ErrorWhenZoneNotFound(t *testing.T) {
	mock := newMockDNSClient()
	solver := solverWithMock(mock)

	ch := newChallengeRequest("nonexistent.com.", testFQDN, "token")
	err := solver.Present(ch)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected to find 1 zone")
}

// --- CleanUp tests ---

func TestCleanUp_DeletesRecordSetWhenSingleRecord(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	mock.recordSets[testZoneID] = []recordsets.RecordSet{
		{
			ID:      "rs-to-delete",
			ZoneID:  testZoneID,
			Name:    testFQDN,
			Type:    "TXT",
			Records: []string{`"only-token"`},
		},
	}
	solver := solverWithMock(mock)

	ch := newChallengeRequest(testZoneName, testFQDN, "only-token")
	err := solver.CleanUp(ch)

	require.NoError(t, err)
	assert.Equal(t, 1, mock.deleteCalls)
	assert.Equal(t, 0, mock.updateCalls)
	assert.Empty(t, mock.recordSets[testZoneID])
}

func TestCleanUp_UpdatesRecordSetWhenMultipleRecords(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	mock.recordSets[testZoneID] = []recordsets.RecordSet{
		{
			ID:      "rs-multi",
			ZoneID:  testZoneID,
			Name:    testFQDN,
			Type:    "TXT",
			Records: []string{`"keep-this"`, `"remove-this"`, `"keep-that"`},
		},
	}
	solver := solverWithMock(mock)

	ch := newChallengeRequest(testZoneName, testFQDN, "remove-this")
	err := solver.CleanUp(ch)

	require.NoError(t, err)
	assert.Equal(t, 0, mock.deleteCalls)
	assert.Equal(t, 1, mock.updateCalls)

	rrs := mock.recordSets[testZoneID]
	require.Len(t, rrs, 1)
	assert.Equal(t, []string{`"keep-this"`, `"keep-that"`}, rrs[0].Records)
}

func TestCleanUp_ErrorWhenNoRecordSetFound(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	solver := solverWithMock(mock)

	ch := newChallengeRequest(testZoneName, testFQDN, "missing-token")
	err := solver.CleanUp(ch)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no TXT recordset found")
}

func TestCleanUp_ErrorWhenZoneNotFound(t *testing.T) {
	mock := newMockDNSClient()
	solver := solverWithMock(mock)

	ch := newChallengeRequest("nonexistent.com.", testFQDN, "token")
	err := solver.CleanUp(ch)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected to find 1 zone")
}

// --- findExistingRecordSet tests ---

func TestFindExistingRecordSet_ReturnsNilWhenNotFound(t *testing.T) {
	mock := newMockDNSClient()
	solver := solverWithMock(mock)

	rs, err := solver.findExistingRecordSet(testZoneID, testFQDN)

	require.NoError(t, err)
	assert.Nil(t, rs)
}

func TestFindExistingRecordSet_ReturnsRecordSet(t *testing.T) {
	mock := newMockDNSClient()
	mock.recordSets[testZoneID] = []recordsets.RecordSet{
		{
			ID:   "found-rs",
			Name: testFQDN,
			Type: "TXT",
		},
	}
	solver := solverWithMock(mock)

	rs, err := solver.findExistingRecordSet(testZoneID, testFQDN)

	require.NoError(t, err)
	require.NotNil(t, rs)
	assert.Equal(t, "found-rs", rs.ID)
}

func TestFindExistingRecordSet_IgnoresNonTXTRecords(t *testing.T) {
	mock := newMockDNSClient()
	mock.recordSets[testZoneID] = []recordsets.RecordSet{
		{
			ID:   "a-record",
			Name: testFQDN,
			Type: "A",
		},
	}
	solver := solverWithMock(mock)

	rs, err := solver.findExistingRecordSet(testZoneID, testFQDN)

	require.NoError(t, err)
	assert.Nil(t, rs)
}

// --- Integration-style: Present then CleanUp ---

func TestPresentThenCleanUp_FullLifecycle(t *testing.T) {
	mock := newMockDNSClient()
	setupMockWithZone(mock)
	solver := solverWithMock(mock)

	ch1 := newChallengeRequest(testZoneName, testFQDN, "token-a")
	ch2 := newChallengeRequest(testZoneName, testFQDN, "token-b")

	require.NoError(t, solver.Present(ch1))
	assert.Len(t, mock.recordSets[testZoneID][0].Records, 1)

	require.NoError(t, solver.Present(ch2))
	assert.Len(t, mock.recordSets[testZoneID][0].Records, 2)

	// Presenting ch1 again should be a no-op
	require.NoError(t, solver.Present(ch1))
	assert.Len(t, mock.recordSets[testZoneID][0].Records, 2)

	// CleanUp ch1 should only remove that record, keeping ch2
	require.NoError(t, solver.CleanUp(ch1))
	require.Len(t, mock.recordSets[testZoneID], 1)
	assert.Equal(t, []string{`"token-b"`}, mock.recordSets[testZoneID][0].Records)

	// CleanUp ch2 should delete the entire recordset
	require.NoError(t, solver.CleanUp(ch2))
	assert.Empty(t, mock.recordSets[testZoneID])
}
