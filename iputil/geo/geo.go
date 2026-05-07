package geo

import (
	"cmp"
	"math"
	"net/netip"

	geoip2 "github.com/oschwald/geoip2-golang/v2"
)

type Reader interface {
	Country(netip.Addr) (Country, error)
	City(netip.Addr) (City, error)
	ASN(netip.Addr) (ASN, error)
	IsEmpty() bool
	HasCountry() bool
	HasCity() bool
	HasASN() bool
}

type Country struct {
	Name string
	ISO  string
	IsEU *bool
}

type City struct {
	Name       string
	Latitude   float64
	Longitude  float64
	PostalCode string
	Timezone   string
	RegionName string
	RegionCode string
}

type ASN struct {
	AutonomousSystemNumber       uint
	AutonomousSystemOrganization string
}

type geoip struct {
	country *geoip2.Reader
	city    *geoip2.Reader
	asn     *geoip2.Reader
}

func Open(countryDB, cityDB string, asnDB string) (Reader, error) {
	var country, city, asn *geoip2.Reader
	if countryDB != "" {
		r, err := geoip2.Open(countryDB)
		if err != nil {
			return nil, err
		}
		country = r
	}
	if cityDB != "" {
		r, err := geoip2.Open(cityDB)
		if err != nil {
			return nil, err
		}
		city = r
	}
	if asnDB != "" {
		r, err := geoip2.Open(asnDB)
		if err != nil {
			return nil, err
		}
		asn = r
	}
	return &geoip{country: country, city: city, asn: asn}, nil
}

func (g *geoip) Country(ip netip.Addr) (Country, error) {
	country := Country{}
	if g.country == nil {
		return country, nil
	}
	record, err := g.country.Country(ip)
	if err != nil {
		return country, err
	}
	country.Name = cmp.Or(record.Country.Names.English, record.RegisteredCountry.Names.English)
	country.ISO = cmp.Or(record.Country.ISOCode, record.RegisteredCountry.ISOCode)
	isEU := record.Country.IsInEuropeanUnion || record.RegisteredCountry.IsInEuropeanUnion
	country.IsEU = &isEU
	return country, nil
}

func (g *geoip) City(ip netip.Addr) (City, error) {
	city := City{}
	if g.city == nil {
		return city, nil
	}
	record, err := g.city.City(ip)
	if err != nil {
		return city, err
	}
	city.Name = record.City.Names.English
	if len(record.Subdivisions) > 0 {
		city.RegionName = record.Subdivisions[0].Names.English
		city.RegionCode = record.Subdivisions[0].ISOCode
	}
	if record.Location.Latitude != nil && !math.IsNaN(*record.Location.Latitude) {
		city.Latitude = *record.Location.Latitude
	}
	if record.Location.Longitude != nil && !math.IsNaN(*record.Location.Longitude) {
		city.Longitude = *record.Location.Longitude
	}
	city.PostalCode = record.Postal.Code
	city.Timezone = record.Location.TimeZone

	return city, nil
}

func (g *geoip) ASN(ip netip.Addr) (ASN, error) {
	asn := ASN{}
	if g.asn == nil {
		return asn, nil
	}
	record, err := g.asn.ASN(ip)
	if err != nil {
		return asn, err
	}
	asn.AutonomousSystemNumber = record.AutonomousSystemNumber
	asn.AutonomousSystemOrganization = record.AutonomousSystemOrganization
	return asn, nil
}

func (g *geoip) IsEmpty() bool {
	return g.country == nil && g.city == nil && g.asn == nil
}

func (g *geoip) HasCountry() bool { return g.country != nil }

func (g *geoip) HasCity() bool { return g.city != nil }

func (g *geoip) HasASN() bool { return g.asn != nil }
