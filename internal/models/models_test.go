package models

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

// Webhook subscriptions have no guild; an empty guildId would be rejected by the GuildIdIndex GSI.
func TestSubscriptionOmitsEmptyGuildID(t *testing.T) {
	item, err := attributevalue.MarshalMap(Subscription{ServerID: "battlenet#us#1", TargetType: "webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := item["guildId"]; ok {
		t.Fatal("empty guildId must not be written")
	}
}
