package service

import "testing"

func TestCatalogueIsValid(t *testing.T) {
	services := Catalogue()

	if len(services) == 0 {
		t.Fatal("Catalogue() is empty")
	}

	if err := ValidateCatalogue(services); err != nil {
		t.Fatalf("ValidateCatalogue(Catalogue()) returned error: %v", err)
	}
}

func TestCataloguePreservesRetainedIdentitiesAndMetadata(t *testing.T) {
	want := []Service{
		{ID: "github", Name: "GitHub", Category: CategoryDeveloperCloud, WebsiteURL: "https://github.com/", CheckURL: "https://github.com/explore", Weight: 10, Enabled: true},
		{ID: "openai", Name: "ChatGPT", Category: CategoryAI, WebsiteURL: "https://chatgpt.com", CheckURL: "https://chatgpt.com/", Weight: 9, Enabled: true},
		{ID: "discord", Name: "Discord", Category: CategoryCommunication, WebsiteURL: "https://discord.com/", CheckURL: "https://discord.com/app", Weight: 8, Enabled: true},
		{ID: "instagram", Name: "Instagram", Category: CategorySocialMedia, WebsiteURL: "https://www.instagram.com/", CheckURL: "https://www.instagram.com/", Weight: 8, Enabled: true},
		{ID: "netflix", Name: "Netflix", Category: CategoryEntertainment, WebsiteURL: "https://www.netflix.com/", CheckURL: "https://www.netflix.com/browse", Weight: 8, Enabled: true},
		{ID: "playstation-network", Name: "PlayStation Network", Category: CategoryGaming, WebsiteURL: "https://www.playstation.com/", CheckURL: "https://store.playstation.com/", Weight: 7, Enabled: true},
		{ID: "amazon", Name: "Amazon", Category: CategoryCommerce, WebsiteURL: "https://www.amazon.com/", CheckURL: "https://www.amazon.com/", Weight: 9, Enabled: true},
	}
	byID := make(map[string]Service)
	for _, svc := range Catalogue() {
		byID[svc.ID] = svc
	}
	for _, expected := range want {
		actual, exists := byID[expected.ID]
		// Metadata remains stable even if a service is disabled later.
		actual.Enabled = expected.Enabled
		if !exists || actual != expected {
			t.Errorf("catalogue identity %q = %+v, want %+v", expected.ID, actual, expected)
		}
	}
}

func TestCatalogueDoesNotReuseRetiredIDs(t *testing.T) {
	for _, svc := range Catalogue() {
		switch svc.ID {
		case "cloudflare", "slack", "google-drive":
			t.Errorf("retired ID %q must not be reused", svc.ID)
		}
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
