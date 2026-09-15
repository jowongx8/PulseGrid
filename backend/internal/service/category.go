package service

type Category string

const (
	CategoryDeveloperCloud Category = "developer-cloud"
	CategoryAI             Category = "ai"
	CategorySocialMedia    Category = "social-media"
	CategoryCommunication  Category = "communication"
	CategoryProductivity   Category = "productivity"
	CategoryEntertainment  Category = "entertainment"
	CategoryGaming         Category = "gaming"
	CategoryCommerce       Category = "commerce"
)

func (category Category) Valid() bool {
	switch category {
	case CategoryDeveloperCloud,
		CategoryAI,
		CategorySocialMedia,
		CategoryCommunication,
		CategoryProductivity,
		CategoryEntertainment,
		CategoryGaming,
		CategoryCommerce:
		return true
	default:
		return false
	}
}
