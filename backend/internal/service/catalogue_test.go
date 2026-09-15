package service

import "testing"

func TestCatalogueIsValid(t *testing.T) {
	services := Catalogue()

	if len(services) < 8 || len(services) > 12 {
		t.Fatalf("len(Catalogue()) = %d, want between 8 and 12", len(services))
	}

	if err := ValidateCatalogue(services); err != nil {
		t.Fatalf("ValidateCatalogue(Catalogue()) returned error: %v", err)
	}
}

func TestCatalogueReturnsCopy(t *testing.T) {
	services := Catalogue()
	services[0].ID = "mutated"

	freshServices := Catalogue()
	if freshServices[0].ID == "mutated" {
		t.Fatal("Catalogue() returned mutable canonical catalogue state")
	}
}

func TestCategoryConstants(t *testing.T) {
	tests := map[Category]string{
		CategoryDeveloperCloud: "developer-cloud",
		CategoryAI:             "ai",
		CategorySocialMedia:    "social-media",
		CategoryCommunication:  "communication",
		CategoryProductivity:   "productivity",
		CategoryEntertainment:  "entertainment",
		CategoryGaming:         "gaming",
		CategoryCommerce:       "commerce",
	}

	for category, want := range tests {
		if string(category) != want {
			t.Fatalf("category = %q, want %q", category, want)
		}

		if !category.Valid() {
			t.Fatalf("category %q is not valid", category)
		}
	}
}

func TestValidateCatalogueRejectsInvalidServices(t *testing.T) {
	tests := []struct {
		name     string
		services []Service
	}{
		{
			name: "duplicate service ID",
			services: []Service{
				validService(),
				func() Service {
					svc := validService()
					svc.Name = "Duplicate GitHub"
					return svc
				}(),
			},
		},
		{
			name: "empty ID",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.ID = ""
				}),
			},
		},
		{
			name: "invalid slug ID",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.ID = "GitHub"
				}),
			},
		},
		{
			name: "empty name",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.Name = ""
				}),
			},
		},
		{
			name: "invalid category",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.Category = Category("unknown")
				}),
			},
		},
		{
			name: "invalid WebsiteURL",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.WebsiteURL = "http://[::1"
				}),
			},
		},
		{
			name: "invalid CheckURL",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.CheckURL = "http://[::1"
				}),
			},
		},
		{
			name: "unsupported URL scheme",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.WebsiteURL = "ftp://example.com"
				}),
			},
		},
		{
			name: "missing URL host",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.WebsiteURL = "https:///status"
				}),
			},
		},
		{
			name: "weight below minimum",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.Weight = 0
				}),
			},
		},
		{
			name: "weight above maximum",
			services: []Service{
				mutateValidService(func(svc *Service) {
					svc.Weight = 11
				}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCatalogue(tt.services); err == nil {
				t.Fatal("ValidateCatalogue() returned nil error, want validation error")
			}
		})
	}
}

func validService() Service {
	return Service{
		ID:         "github",
		Name:       "GitHub",
		Category:   CategoryDeveloperCloud,
		WebsiteURL: "https://github.com",
		CheckURL:   "https://github.com",
		Weight:     10,
		Enabled:    true,
	}
}

func mutateValidService(mutate func(*Service)) Service {
	svc := validService()
	mutate(&svc)

	return svc
}
