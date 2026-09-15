package service

type Service struct {
	ID         string
	Name       string
	Category   Category
	WebsiteURL string
	CheckURL   string
	Weight     int
	Enabled    bool
}
