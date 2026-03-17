package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gophercloud/gophercloud/openstack/dns/v2/recordsets"
	"github.com/gophercloud/gophercloud/openstack/dns/v2/zones"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
)

const GroupName = "acme.syseleven.de"

func main() {
	cmd.RunWebhookServer(GroupName,
		&designateDNSProviderSolver{},
	)
}

type designateDNSProviderSolver struct {
	client dnsClient
}

func New() webhook.Solver {
	client, err := createDesignateServiceClient()
	if err != nil {
		panic(fmt.Errorf("%v", err))
	}
	return &designateDNSProviderSolver{
		client: &gophercloudDNSClient{sc: client},
	}
}

func (c *designateDNSProviderSolver) Name() string {
	return "designatedns"
}

func (c *designateDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	log.Debugf("Present() called ch.DNSName=%s ch.ResolvedZone=%s ch.ResolvedFQDN=%s ch.Type=%s", ch.DNSName, ch.ResolvedZone, ch.ResolvedFQDN, ch.Type)

	zoneID, err := c.getZoneID(ch.ResolvedZone)
	if err != nil {
		return fmt.Errorf("Present: %w", err)
	}

	quotedKey := quoteRecord(ch.Key)

	existing, err := c.findExistingRecordSet(zoneID, ch.ResolvedFQDN)
	if err != nil {
		return err
	}

	if existing != nil {
		for _, r := range existing.Records {
			if r == quotedKey {
				log.Debugf("Record %s already exists in recordset %s, skipping", quotedKey, existing.ID)
				return nil
			}
		}
		opts := buildUpdateOpts(existing, append(existing.Records, quotedKey))
		_, err = c.client.UpdateRecordSet(zoneID, existing.ID, opts)
		return err
	}

	opts := recordsets.CreateOpts{
		Name:    ch.ResolvedFQDN,
		Type:    "TXT",
		Records: []string{quotedKey},
	}
	_, err = c.client.CreateRecordSet(zoneID, opts)
	return err
}

func (c *designateDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	log.Debugf("CleanUp called ch.ResolvedZone=%s ch.ResolvedFQDN=%s", ch.ResolvedZone, ch.ResolvedFQDN)

	zoneID, err := c.getZoneID(ch.ResolvedZone)
	if err != nil {
		return fmt.Errorf("CleanUp: %w", err)
	}

	quotedKey := quoteRecord(ch.Key)

	existing, err := c.findExistingRecordSet(zoneID, ch.ResolvedFQDN)
	if err != nil {
		return err
	}

	if existing == nil {
		return fmt.Errorf("CleanUp: no TXT recordset found for %s in zone %s", ch.ResolvedFQDN, ch.ResolvedZone)
	}

	var remaining []string
	for _, r := range existing.Records {
		if r != quotedKey {
			remaining = append(remaining, r)
		}
	}

	if len(remaining) == 0 {
		return c.client.DeleteRecordSet(zoneID, existing.ID)
	}

	opts := buildUpdateOpts(existing, remaining)
	_, err = c.client.UpdateRecordSet(zoneID, existing.ID, opts)
	return err
}

func (c *designateDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	log.Debugf("Initialize called")

	cl, err := createDesignateServiceClient()
	if err != nil {
		return err
	}

	c.client = &gophercloudDNSClient{sc: cl}
	return nil
}

func (c *designateDNSProviderSolver) getZoneID(resolvedZone string) (string, error) {
	allZones, err := c.client.ListZones(zones.ListOpts{Name: resolvedZone})
	if err != nil {
		return "", err
	}
	if len(allZones) != 1 {
		return "", fmt.Errorf("expected to find 1 zone %s, found %d", resolvedZone, len(allZones))
	}
	return allZones[0].ID, nil
}

func (c *designateDNSProviderSolver) findExistingRecordSet(zoneID, fqdn string) (*recordsets.RecordSet, error) {
	opts := recordsets.ListOpts{
		Name: fqdn,
		Type: "TXT",
	}
	allRRs, err := c.client.ListRecordSetsByZone(zoneID, opts)
	if err != nil {
		return nil, err
	}
	if len(allRRs) == 0 {
		return nil, nil
	}
	return &allRRs[0], nil
}

// fullUpdateOpts extends the standard UpdateOpts with a Type field so the PUT
// body matches what strict API gateways expect (description + ttl + type + records).
type fullUpdateOpts struct {
	Description string   `json:"description"`
	TTL         int      `json:"ttl"`
	Type        string   `json:"type"`
	Records     []string `json:"records"`
}

func (o fullUpdateOpts) ToRecordSetUpdateMap() (map[string]interface{}, error) {
	return map[string]interface{}{
		"description": o.Description,
		"ttl":         o.TTL,
		"type":        o.Type,
		"records":     o.Records,
	}, nil
}

func buildUpdateOpts(existing *recordsets.RecordSet, records []string) fullUpdateOpts {
	return fullUpdateOpts{
		Description: existing.Description,
		TTL:         existing.TTL,
		Type:        existing.Type,
		Records:     records,
	}
}

func quoteRecord(r string) string {
	if strings.HasPrefix(r, "\"") && strings.HasSuffix(r, "\"") {
		return r
	} else {
		return strconv.Quote(r)
	}
}
