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
			WebsiteURL: "https://github.com/",
			CheckURL:   "https://github.com/explore",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "aws",
			Name:       "AWS",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://aws.amazon.com/",
			CheckURL:   "https://console.aws.amazon.com/",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "vercel",
			Name:       "Vercel",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://vercel.com/",
			CheckURL:   "https://vercel.com/login",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "npm",
			Name:       "npm",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://www.npmjs.com/",
			CheckURL:   "https://registry.npmjs.org/-/ping",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "supabase",
			Name:       "Supabase",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://supabase.com/",
			CheckURL:   "https://supabase.com/dashboard",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "digitalocean",
			Name:       "DigitalOcean",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://www.digitalocean.com/",
			CheckURL:   "https://cloud.digitalocean.com/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "docker-hub",
			Name:       "Docker Hub",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://hub.docker.com/",
			CheckURL:   "https://hub.docker.com/search",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "replit",
			Name:       "Replit",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://replit.com/",
			CheckURL:   "https://replit.com/login",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "stackblitz",
			Name:       "StackBlitz",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://stackblitz.com/",
			CheckURL:   "https://stackblitz.com/edit/vitejs-vite",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "netlify",
			Name:       "Netlify",
			Category:   CategoryDeveloperCloud,
			WebsiteURL: "https://www.netlify.com/",
			CheckURL:   "https://app.netlify.com/login",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "openai",
			Name:       "ChatGPT",
			Category:   CategoryAI,
			WebsiteURL: "https://chatgpt.com",
			CheckURL:   "https://chatgpt.com/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "grok",
			Name:       "Grok",
			Category:   CategoryAI,
			WebsiteURL: "https://grok.com/",
			CheckURL:   "https://grok.com/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "huggingchat",
			Name:       "HuggingChat",
			Category:   CategoryAI,
			WebsiteURL: "https://huggingface.co/chat/",
			CheckURL:   "https://huggingface.co/chat/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "duck-ai",
			Name:       "Duck.ai",
			Category:   CategoryAI,
			WebsiteURL: "https://duck.ai/",
			CheckURL:   "https://duck.ai/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "suno",
			Name:       "Suno",
			Category:   CategoryAI,
			WebsiteURL: "https://suno.com/",
			CheckURL:   "https://suno.com/create",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "kimi",
			Name:       "Kimi",
			Category:   CategoryAI,
			WebsiteURL: "https://www.kimi.com/",
			CheckURL:   "https://www.kimi.com/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "qwen",
			Name:       "Qwen",
			Category:   CategoryAI,
			WebsiteURL: "https://chat.qwen.ai/",
			CheckURL:   "https://chat.qwen.ai/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "discord",
			Name:       "Discord",
			Category:   CategoryCommunication,
			WebsiteURL: "https://discord.com/",
			CheckURL:   "https://discord.com/app",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "whatsapp",
			Name:       "WhatsApp",
			Category:   CategoryCommunication,
			WebsiteURL: "https://www.whatsapp.com/",
			CheckURL:   "https://web.whatsapp.com/",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "telegram",
			Name:       "Telegram",
			Category:   CategoryCommunication,
			WebsiteURL: "https://web.telegram.org/",
			CheckURL:   "https://web.telegram.org/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "zoom",
			Name:       "Zoom",
			Category:   CategoryCommunication,
			WebsiteURL: "https://zoom.us/",
			CheckURL:   "https://app.zoom.us/wc/",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "element",
			Name:       "Element",
			Category:   CategoryCommunication,
			WebsiteURL: "https://element.io/",
			CheckURL:   "https://app.element.io/",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "jitsi-meet",
			Name:       "Jitsi Meet",
			Category:   CategoryCommunication,
			WebsiteURL: "https://meet.jit.si/",
			CheckURL:   "https://meet.jit.si/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "instagram",
			Name:       "Instagram",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://www.instagram.com/",
			CheckURL:   "https://www.instagram.com/",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "facebook",
			Name:       "Facebook",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://www.facebook.com/",
			CheckURL:   "https://www.facebook.com/",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "tiktok",
			Name:       "TikTok",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://www.tiktok.com/",
			CheckURL:   "https://www.tiktok.com/explore",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "threads",
			Name:       "Threads",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://www.threads.com/",
			CheckURL:   "https://www.threads.com/",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "bluesky",
			Name:       "Bluesky",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://bsky.app/",
			CheckURL:   "https://bsky.app/",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "mastodon-social",
			Name:       "Mastodon.social",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://mastodon.social/",
			CheckURL:   "https://mastodon.social/explore",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "tumblr",
			Name:       "Tumblr",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://www.tumblr.com/",
			CheckURL:   "https://www.tumblr.com/explore/trending",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "x",
			Name:       "X",
			Category:   CategorySocialMedia,
			WebsiteURL: "https://x.com/",
			CheckURL:   "https://x.com/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "notion",
			Name:       "Notion",
			Category:   CategoryProductivity,
			WebsiteURL: "https://www.notion.com/",
			CheckURL:   "https://www.notion.com/login",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "figma",
			Name:       "Figma",
			Category:   CategoryProductivity,
			WebsiteURL: "https://www.figma.com/",
			CheckURL:   "https://www.figma.com/login",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "canva",
			Name:       "Canva",
			Category:   CategoryProductivity,
			WebsiteURL: "https://www.canva.com/",
			CheckURL:   "https://www.canva.com/login/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "dropbox",
			Name:       "Dropbox",
			Category:   CategoryProductivity,
			WebsiteURL: "https://www.dropbox.com/",
			CheckURL:   "https://www.dropbox.com/home",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "linear",
			Name:       "Linear",
			Category:   CategoryProductivity,
			WebsiteURL: "https://linear.app/",
			CheckURL:   "https://linear.app/login",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "clickup",
			Name:       "ClickUp",
			Category:   CategoryProductivity,
			WebsiteURL: "https://clickup.com/",
			CheckURL:   "https://app.clickup.com/login",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "miro",
			Name:       "Miro",
			Category:   CategoryProductivity,
			WebsiteURL: "https://miro.com/",
			CheckURL:   "https://miro.com/login/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "box",
			Name:       "Box",
			Category:   CategoryProductivity,
			WebsiteURL: "https://www.box.com/",
			CheckURL:   "https://account.box.com/login",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "bitwarden",
			Name:       "Bitwarden",
			Category:   CategoryProductivity,
			WebsiteURL: "https://bitwarden.com/",
			CheckURL:   "https://vault.bitwarden.com/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "youtube",
			Name:       "YouTube",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://www.youtube.com/",
			CheckURL:   "https://www.youtube.com/",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "spotify",
			Name:       "Spotify",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://open.spotify.com/",
			CheckURL:   "https://open.spotify.com/",
			Weight:     10,
			Enabled:    true,
		},
		{
			ID:         "twitch",
			Name:       "Twitch",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://www.twitch.tv/",
			CheckURL:   "https://www.twitch.tv/",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "apple-music",
			Name:       "Apple Music",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://music.apple.com/",
			CheckURL:   "https://music.apple.com/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "soundcloud",
			Name:       "SoundCloud",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://soundcloud.com/",
			CheckURL:   "https://soundcloud.com/discover",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "netflix",
			Name:       "Netflix",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://www.netflix.com/",
			CheckURL:   "https://www.netflix.com/browse",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "crunchyroll",
			Name:       "Crunchyroll",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://www.crunchyroll.com/",
			CheckURL:   "https://www.crunchyroll.com/videos/popular",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "vimeo",
			Name:       "Vimeo",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://vimeo.com/",
			CheckURL:   "https://vimeo.com/watch",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "mixcloud",
			Name:       "Mixcloud",
			Category:   CategoryEntertainment,
			WebsiteURL: "https://www.mixcloud.com/",
			CheckURL:   "https://www.mixcloud.com/genres/",
			Weight:     6,
			Enabled:    true,
		},
		{
			ID:         "playstation-network",
			Name:       "PlayStation Network",
			Category:   CategoryGaming,
			WebsiteURL: "https://www.playstation.com/",
			CheckURL:   "https://store.playstation.com/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "steam",
			Name:       "Steam",
			Category:   CategoryGaming,
			WebsiteURL: "https://store.steampowered.com/",
			CheckURL:   "https://store.steampowered.com/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "roblox",
			Name:       "Roblox",
			Category:   CategoryGaming,
			WebsiteURL: "https://www.roblox.com/",
			CheckURL:   "https://www.roblox.com/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "chess-com",
			Name:       "Chess.com",
			Category:   CategoryGaming,
			WebsiteURL: "https://www.chess.com/",
			CheckURL:   "https://www.chess.com/play/online",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "lichess",
			Name:       "Lichess",
			Category:   CategoryGaming,
			WebsiteURL: "https://lichess.org/",
			CheckURL:   "https://lichess.org/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "gog",
			Name:       "GOG",
			Category:   CategoryGaming,
			WebsiteURL: "https://www.gog.com/",
			CheckURL:   "https://www.gog.com/en/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "itch-io",
			Name:       "itch.io",
			Category:   CategoryGaming,
			WebsiteURL: "https://itch.io/",
			CheckURL:   "https://itch.io/games",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "amazon",
			Name:       "Amazon",
			Category:   CategoryCommerce,
			WebsiteURL: "https://www.amazon.com/",
			CheckURL:   "https://www.amazon.com/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "stripe",
			Name:       "Stripe",
			Category:   CategoryCommerce,
			WebsiteURL: "https://stripe.com/",
			CheckURL:   "https://dashboard.stripe.com/",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "shopee",
			Name:       "Shopee",
			Category:   CategoryCommerce,
			WebsiteURL: "https://shopee.com.my/",
			CheckURL:   "https://shopee.com.my/",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "aliexpress",
			Name:       "AliExpress",
			Category:   CategoryCommerce,
			WebsiteURL: "https://www.aliexpress.com/",
			CheckURL:   "https://www.aliexpress.com/",
			Weight:     8,
			Enabled:    true,
		},
		{
			ID:         "wise",
			Name:       "Wise",
			Category:   CategoryCommerce,
			WebsiteURL: "https://wise.com/",
			CheckURL:   "https://wise.com/login/",
			Weight:     7,
			Enabled:    true,
		},
		{
			ID:         "walmart",
			Name:       "Walmart",
			Category:   CategoryCommerce,
			WebsiteURL: "https://www.walmart.com/",
			CheckURL:   "https://www.walmart.com/",
			Weight:     9,
			Enabled:    true,
		},
		{
			ID:         "lazada-my",
			Name:       "Lazada Malaysia",
			Category:   CategoryCommerce,
			WebsiteURL: "https://www.lazada.com.my/",
			CheckURL:   "https://www.lazada.com.my/",
			Weight:     8,
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
