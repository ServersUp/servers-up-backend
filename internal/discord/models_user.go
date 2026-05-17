package discord

// InteractionUser is a minimal Discord user object on interactions.
type InteractionUser struct {
	ID string `json:"id"`
}

// InteractionMember is the guild member who invoked a command.
type InteractionMember struct {
	User InteractionUser `json:"user"`
}

// InvokerUserID returns the Discord user ID of whoever ran the interaction.
// Guild commands use member.user; DMs and some contexts use user.
func (i Interaction) InvokerUserID() string {
	if i.Member != nil && i.Member.User.ID != "" {
		return i.Member.User.ID
	}
	if i.User != nil && i.User.ID != "" {
		return i.User.ID
	}
	return ""
}
