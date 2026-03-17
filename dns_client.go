package main

import (
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/dns/v2/recordsets"
	"github.com/gophercloud/gophercloud/openstack/dns/v2/zones"
)

type dnsClient interface {
	ListZones(opts zones.ListOptsBuilder) ([]zones.Zone, error)
	ListRecordSetsByZone(zoneID string, opts recordsets.ListOptsBuilder) ([]recordsets.RecordSet, error)
	CreateRecordSet(zoneID string, opts recordsets.CreateOptsBuilder) (*recordsets.RecordSet, error)
	UpdateRecordSet(zoneID string, rrsetID string, opts recordsets.UpdateOptsBuilder) (*recordsets.RecordSet, error)
	DeleteRecordSet(zoneID string, rrsetID string) error
}

type gophercloudDNSClient struct {
	sc *gophercloud.ServiceClient
}

func (g *gophercloudDNSClient) ListZones(opts zones.ListOptsBuilder) ([]zones.Zone, error) {
	allPages, err := zones.List(g.sc, opts).AllPages()
	if err != nil {
		return nil, err
	}
	return zones.ExtractZones(allPages)
}

func (g *gophercloudDNSClient) ListRecordSetsByZone(zoneID string, opts recordsets.ListOptsBuilder) ([]recordsets.RecordSet, error) {
	allPages, err := recordsets.ListByZone(g.sc, zoneID, opts).AllPages()
	if err != nil {
		return nil, err
	}
	return recordsets.ExtractRecordSets(allPages)
}

func (g *gophercloudDNSClient) CreateRecordSet(zoneID string, opts recordsets.CreateOptsBuilder) (*recordsets.RecordSet, error) {
	return recordsets.Create(g.sc, zoneID, opts).Extract()
}

func (g *gophercloudDNSClient) UpdateRecordSet(zoneID string, rrsetID string, opts recordsets.UpdateOptsBuilder) (*recordsets.RecordSet, error) {
	return recordsets.Update(g.sc, zoneID, rrsetID, opts).Extract()
}

func (g *gophercloudDNSClient) DeleteRecordSet(zoneID string, rrsetID string) error {
	return recordsets.Delete(g.sc, zoneID, rrsetID).ExtractErr()
}
