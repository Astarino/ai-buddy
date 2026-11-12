package buddy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrustedDirectoryValidatesIdentityAndResourceMappings(t *testing.T) {
	base := LoadCatalog()
	for i := range base.Profiles {
		base.Profiles[i].Issuer = "https://idp.example"
		base.Profiles[i].Subject = "subject-" + base.Profiles[i].ID
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	write := func(c Catalog, suffix string) {
		t.Helper()
		raw, _ := json.Marshal(c)
		if err := os.WriteFile(path, append(raw, []byte(suffix)...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(base, "")
	loaded, err := ReadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := loaded.CorporateProfile("https://idp.example", "subject-anton")
	if err != nil || p.ID != "anton" {
		t.Fatal(p, err)
	}
	if _, err := loaded.CorporateProfile("https://another.example", "subject-anton"); err != ErrForbidden {
		t.Fatal("issuer confusion", err)
	}
	if _, err := loaded.CorporateProfile("https://idp.example", "unknown-subject"); err != ErrForbidden {
		t.Fatal("unknown identity provisioned", err)
	}
	changed := p
	changed.Team = "different-team"
	if ProfileBinding(changed) == ProfileBinding(p) {
		t.Fatal("session binding ignores authority change")
	}
	for _, alter := range []func(*Catalog){
		func(c *Catalog) { c.Profiles = nil },
		func(c *Catalog) { c.Profiles[0].Subject = "" },
		func(c *Catalog) { c.Profiles[1].ID = c.Profiles[0].ID },
		func(c *Catalog) { c.Profiles[1].Subject = c.Profiles[0].Subject },
		func(c *Catalog) { c.Documents[1].ID = c.Documents[0].ID },
		func(c *Catalog) { c.Documents[0].Version = "" },
		func(c *Catalog) { c.Documents[0].Resource = "" },
	} {
		raw, _ := json.Marshal(base)
		var copy Catalog
		json.Unmarshal(raw, &copy)
		alter(&copy)
		write(copy, "")
		if _, err := ReadCatalog(path); err == nil {
			t.Fatal("invalid catalog accepted")
		}
	}
	write(base, " {}")
	if _, err := ReadCatalog(path); err == nil {
		t.Fatal("extra JSON accepted")
	}
	os.WriteFile(path, []byte(`{"unknown":true}`), 0600)
	if _, err := ReadCatalog(path); err == nil {
		t.Fatal("unknown schema accepted")
	}
	if _, err := ReadCatalog(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing registry ignored")
	}
}
