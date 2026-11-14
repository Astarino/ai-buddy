package buddy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func ReadCatalog(path string) (Catalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return Catalog{}, err
	}
	defer file.Close()
	var c Catalog
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&c); err != nil {
		return Catalog{}, fmt.Errorf("invalid catalog")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Catalog{}, fmt.Errorf("invalid catalog suffix")
	}
	profiles, documents := map[string]bool{}, map[string]bool{}
	for _, p := range c.Profiles {
		if p.ID == "" || profiles[p.ID] || p.Issuer == "" || p.Subject == "" || p.Direction == "" || p.Name == "" || p.Team == "" || p.Role == "" || p.Space == "" || p.Project == "" {
			return Catalog{}, fmt.Errorf("invalid trusted profile")
		}
		profiles[p.ID] = true
	}
	identities := map[string]bool{}
	for _, p := range c.Profiles {
		key := p.Issuer + "\x00" + p.Subject
		if identities[key] {
			return Catalog{}, fmt.Errorf("duplicate corporate identity")
		}
		identities[key] = true
	}
	for _, d := range c.Documents {
		if d.ID == "" || documents[d.ID] || d.Version == "" {
			return Catalog{}, fmt.Errorf("invalid source identity")
		}
		if d.System == "" || d.ResourceKind == "" || d.Resource == "" {
			return Catalog{}, fmt.Errorf("protected source needs a resource")
		}
		documents[d.ID] = true
	}
	if len(profiles) == 0 {
		return Catalog{}, fmt.Errorf("trusted catalog is empty")
	}
	return c, nil
}

func (c Catalog) CorporateProfile(issuer, subject string) (Profile, error) {
	for _, p := range c.Profiles {
		if p.Issuer != "" && p.Issuer == issuer && p.Subject == subject {
			return p, nil
		}
	}
	return Profile{}, ErrForbidden
}

func ProfileBinding(p Profile) string {
	return fingerprint([]string{p.ID, p.Issuer, p.Subject, p.Team, p.Role, p.Direction, p.Space, p.Project, p.Audience})
}
