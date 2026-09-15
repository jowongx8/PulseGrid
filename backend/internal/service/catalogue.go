package service

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	minWeight = 1
	maxWeight = 10
)

var (
	serviceIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

	catalogue = []Service{
		{
			ID:         "github",
			Name:       "GitHub",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://github.com",
			CheckURL:   "https://github.com",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "cloudflare",
			Name:       "Cloudflare",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://www.cloudflare.com",
			CheckURL:   "https://www.cloudflare.com",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "openai",
			Name:       "OpenAI",
			Category:   CategoryAI,
			WebsiteURL: "https://openai.com",
			CheckURL:   "https://openai.com",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "instagram",
			Name:       "Instagram",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://www.instagram.com",
			CheckURL:   "https://www.instagram.com",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "discord",
			Name:       "Discord",
			Category:   CategoryCommunication,
			WebsiteURL: "https://discord.com",
			CheckURL:   "https://discord.com",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "slack",
			Name:       "Slack",
			Category:   CategoryCommunication,
			WebsiteURL: "https://slack.com",
			CheckURL:   "https://slack.com",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "google-drive",
			Name:       "Google Drive",
			Category:   CategoryProductivity,
			WebsiteURL: "https://drive.google.com",
			CheckURL:   "https://drive.google.com",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "netflix",
			Name:       "Netflix",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://www.netflix.com",
			CheckURL:   "https://www.netflix.com",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "playstation-network",
			Name:       "PlayStation Network",
			Category:   CategoryGaming,
			WebsiteURL: "https://www.playstation.com",
			CheckURL:   "https://www.playstation.com",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "amazon",
			Name:       "Amazon",
			Category:   CategoryCommerce,
			WebsiteURL: "https://www.amazon.com",
			CheckURL:   "https://www.amazon.com",
			Weight:     9,
			Enabled:    true,
		},
	}
)

func Catalogue() []Service {
	services := make([]Service, len(catalogue))
	copy(services, catalogue)

	return services
}

func ValidateCatalogue(services []Service) error {
	seenIDs := make(map[string]struct{}, len(services))

	for index, svc := range services {
		if err := validateService(svc, index, seenIDs); err != nil {
			return err
		}
	}

	return nil
}

func validateService(svc Service, index int, seenIDs map[string]struct{}) error {
	label := fmt.Sprintf("service at index %d", index)
	if svc.ID != "" {
		label = fmt.Sprintf("service %q", svc.ID)
	}

	if svc.ID == "" {
		return fmt.Errorf("%s: ID is required", label)
	}

	if !serviceIDPattern.MatchString(svc.ID) {
		return fmt.Errorf("%s: ID must be a lowercase slug", label)
	}

	if _, exists := seenIDs[svc.ID]; exists {
		return fmt.Errorf("%s: duplicate ID", label)
	}
	seenIDs[svc.ID] = struct{}{}

	if strings.TrimSpace(svc.Name) == "" {
		return fmt.Errorf("%s: Name is required", label)
	}

	if !svc.Category.Valid() {
		return fmt.Errorf("%s: Category %q is not supported", label, svc.Category)
	}

	if err := validateServiceURL(svc.WebsiteURL); err != nil {
		return fmt.Errorf("%s: WebsiteURL: %w", label, err)
	}

	if err := validateServiceURL(svc.CheckURL); err != nil {
		return fmt.Errorf("%s: CheckURL: %w", label, err)
	}

	if svc.Weight < minWeight || svc.Weight > maxWeight {
		return fmt.Errorf("%s: Weight must be between %d and %d", label, minWeight, maxWeight)
	}

	return nil
}

func validateServiceURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("must parse successfully: %w", err)
	}

	if !parsedURL.IsAbs() {
		return fmt.Errorf("must be absolute")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("must include a host")
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("must use http or https")
	}

	return nil
}
