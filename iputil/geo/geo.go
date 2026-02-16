package geo

import (
	"math"
	"net/netip"

	geoip2 "github.com/oschwald/geoip2-golang/v2"
)

type Reader interface {
	Country(netip.Addr) (Country, error)
	City(netip.Addr) (City, error)
	ASN(netip.Addr) (ASN, error)
	IsEmpty() bool
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
	if record.Country.Names.English != "" {
		country.Name = record.Country.Names.English
	}
	if record.RegisteredCountry.Names.English != "" && country.Name == "" {
		country.Name = record.RegisteredCountry.Names.English
	}
	if record.Country.ISOCode != "" {
		country.ISO = record.Country.ISOCode
	}
	if record.RegisteredCountry.ISOCode != "" && country.ISO == "" {
		country.ISO = record.RegisteredCountry.ISOCode
	}
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
	if record.City.Names.English != "" {
		city.Name = record.City.Names.English
	}
	if len(record.Subdivisions) > 0 {
		if record.Subdivisions[0].Names.English != "" {
			city.RegionName = record.Subdivisions[0].Names.English
		}
		if record.Subdivisions[0].ISOCode != "" {
			city.RegionCode = record.Subdivisions[0].ISOCode
		}
	}
	if record.Location.Latitude != nil && !math.IsNaN(*record.Location.Latitude) {
		city.Latitude = *record.Location.Latitude
	}
	if record.Location.Longitude != nil && !math.IsNaN(*record.Location.Longitude) {
		city.Longitude = *record.Location.Longitude
	}
	if record.Postal.Code != "" {
		city.PostalCode = record.Postal.Code
	}
	if record.Location.TimeZone != "" {
		city.Timezone = record.Location.TimeZone
	}

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
	if record.AutonomousSystemNumber > 0 {
		asn.AutonomousSystemNumber = record.AutonomousSystemNumber
	}
	if record.AutonomousSystemOrganization != "" {
		asn.AutonomousSystemOrganization = record.AutonomousSystemOrganization
	}
	return asn, nil
}

func (g *geoip) IsEmpty() bool {
	return g.country == nil && g.city == nil
}
