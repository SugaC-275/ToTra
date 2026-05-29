package middleware

import "regexp"

// ContentRating represents an MPAA-style content rating level.
// Ordered from most restrictive to least: G < PG < PG-13 < R < unrated.
type ContentRating string

const (
	RatingG       ContentRating = "G"
	RatingPG      ContentRating = "PG"
	RatingPG13    ContentRating = "PG-13"
	RatingR       ContentRating = "R"
	RatingUnrated ContentRating = "unrated"
)

// ratingOrder maps each rating to a numeric level for comparison.
var ratingOrder = map[ContentRating]int{
	RatingG:       0,
	RatingPG:      1,
	RatingPG13:    2,
	RatingR:       3,
	RatingUnrated: 4,
}

// RatingAllows reports whether a model configured with modelRating is permitted
// to handle content at the requestedLevel.
// Unrated models allow all content. A G-rated model rejects PG and above.
func RatingAllows(modelRating ContentRating, requestedLevel ContentRating) bool {
	if modelRating == RatingUnrated {
		return true
	}
	modelLevel, mOk := ratingOrder[modelRating]
	requestedLvl, rOk := ratingOrder[requestedLevel]
	if !mOk || !rOk {
		return true // unknown ratings: allow
	}
	return requestedLvl <= modelLevel
}

type ratingSignal struct {
	rating ContentRating
	re     *regexp.Regexp
}

// ratingSignals are ordered from strongest to weakest. DetectRequestedRating
// returns the highest-matched rating.
var ratingSignals = []*ratingSignal{
	// R-level signals
	{
		rating: RatingR,
		re:     regexp.MustCompile(`(?i)(?:explicit\s+(?:violence|gore|sexual|nudity)|graphic\s+(?:violence|gore|murder|torture)|strong\s+language|adult\s+(?:content|material|film)|pornograph|erotic(?:a)?|hentai|hardcore)`),
	},

	// PG-13 signals
	{
		rating: RatingPG13,
		re:     regexp.MustCompile(`(?i)(?:moderate\s+violence|suggestive\s+(?:content|themes?|material)|sexual\s+(?:innuendo|suggestion|reference)|intense\s+(?:action|sequences?|themes?)|brief\s+nudity|partial\s+nudity|drug\s+use)`),
	},

	// PG signals
	{
		rating: RatingPG,
		re:     regexp.MustCompile(`(?i)(?:mild\s+violence|scary\s+(?:themes?|scenes?|content)|some\s+violence|action\s+(?:violence|sequences?)|frightening\s+(?:scenes?|moments?|images?))`),
	},
}

// DetectRequestedRating returns a best-guess ContentRating for the given request
// body bytes based on presence of content-level keywords.
// Returns RatingG when no signals are detected.
func DetectRequestedRating(body []byte) ContentRating {
	text := string(body)
	for _, sig := range ratingSignals {
		if sig.re.MatchString(text) {
			return sig.rating
		}
	}
	return RatingG
}
