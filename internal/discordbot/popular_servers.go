package discordbot

// supportedGamesListURL is the public page listing all configured servers per game.
const supportedGamesListURL = "https://serversup.github.io/#games"

// maxInlineServerNames is how many server keys we list in one Discord message before abbreviating.
const maxInlineServerNames = 25

// wowPopularServerKeys are well-known US realm keys (normalized, lowercase, hyphenated).
// Shown when the full wow server list is too long for a single message.
var wowPopularServerKeys = []string{
	"illidan",
	"area-52",
	"stormrage",
	"tichondrius",
	"proudmoore",
	"sargeras",
	"malganis",
	"thrall",
	"zuljin",
	"moon-guard",
}
