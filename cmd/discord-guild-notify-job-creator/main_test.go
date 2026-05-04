package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ServersUp/servers-up-backend/internal/models"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type mockLister struct {
	subs []models.Subscription
	err  error
}

func (m *mockLister) ListSubscriptionsByServer(ctx context.Context, serverID string) ([]models.Subscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.subs, nil
}

type mockSQS struct {
	bodies    []string
	failAfter int // 0 = never fail; N = fail on Nth SendMessage call (1-based)
	calls     int
	err       error
}

func (m *mockSQS) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	if m.failAfter > 0 && m.calls == m.failAfter {
		return nil, errors.New("injected sqs failure")
	}
	if params.MessageBody != nil {
		m.bodies = append(m.bodies, *params.MessageBody)
	}
	return &sqs.SendMessageOutput{}, nil
}

func TestRoleIDFromMention(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"hello", ""},
		{"<@&123456789>", "123456789"},
		{"ping <@&99> ok", "99"},
		{"<@123456789>", ""},
	}
	for _, tc := range tests {
		got := roleIDFromMention(tc.in)
		if got != tc.want {
			t.Fatalf("roleIDFromMention(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringAttr(t *testing.T) {
	t.Parallel()
	img := map[string]events.DynamoDBAttributeValue{
		"serverId": events.NewStringAttribute("battlenet#us#11"),
		"n":        events.NewNumberAttribute("42"),
	}
	if got := stringAttr(img, "serverId"); got != "battlenet#us#11" {
		t.Fatalf("serverId: got %q", got)
	}
	if got := stringAttr(img, "missing"); got != "" {
		t.Fatalf("missing: got %q", got)
	}
	if got := stringAttr(nil, "serverId"); got != "" {
		t.Fatalf("nil map: got %q", got)
	}
	if got := stringAttr(img, "n"); got != "" {
		t.Fatalf("non-string type should yield empty: got %q", got)
	}
}

func statusChangeRecord(oldStatus, newStatus string) events.DynamoDBEventRecord {
	return events.DynamoDBEventRecord{
		EventName: string(events.DynamoDBOperationTypeModify),
		EventID:   "evt-1",
		Change: events.DynamoDBStreamRecord{
			OldImage: map[string]events.DynamoDBAttributeValue{
				"serverId": events.NewStringAttribute("battlenet#us#11"),
				"status":   events.NewStringAttribute(oldStatus),
			},
			NewImage: map[string]events.DynamoDBAttributeValue{
				"serverId": events.NewStringAttribute("battlenet#us#11"),
				"status":   events.NewStringAttribute(newStatus),
			},
		},
	}
}

func TestProcessRecord_UP_to_DOWN_enqueuesJobs(t *testing.T) {
	t.Parallel()
	ml := &mockLister{
		subs: []models.Subscription{
			{ServerID: "battlenet#us#11", GuildID: "g1", ChannelID: "c1", Mention: ""},
			{ServerID: "battlenet#us#11", GuildID: "g1", ChannelID: "c2", Mention: "<@&777>"},
		},
	}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}

	rec := statusChangeRecord("UP", "DOWN")
	if err := h.processRecord(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	if len(ms.bodies) != 2 {
		t.Fatalf("expected 2 sqs messages, got %d", len(ms.bodies))
	}
	var j0, j1 models.GuildNotifyJob
	if err := json.Unmarshal([]byte(ms.bodies[0]), &j0); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(ms.bodies[1]), &j1); err != nil {
		t.Fatal(err)
	}
	if j0.ServerID != "battlenet#us#11" || j0.Status != "DOWN" || j0.GuildID != "g1" || j0.ChannelID != "c1" || j0.RoleID != "" {
		t.Fatalf("unexpected job0: %+v", j0)
	}
	if j1.RoleID != "777" || j1.ChannelID != "c2" {
		t.Fatalf("unexpected job1: %+v", j1)
	}
}

func TestProcessRecord_DOWN_to_UP(t *testing.T) {
	t.Parallel()
	ml := &mockLister{
		subs: []models.Subscription{
			{GuildID: "g", ChannelID: "ch", Mention: ""},
		},
	}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("DOWN", "UP")
	if err := h.processRecord(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	var job models.GuildNotifyJob
	_ = json.Unmarshal([]byte(ms.bodies[0]), &job)
	if job.Status != "UP" {
		t.Fatalf("status: %+v", job)
	}
}

func TestProcessRecord_noSubscriptions(t *testing.T) {
	t.Parallel()
	ml := &mockLister{subs: nil}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("UP", "DOWN")
	if err := h.processRecord(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	if len(ms.bodies) != 0 {
		t.Fatalf("expected no messages, got %v", ms.bodies)
	}
}

func TestProcessRecord_listError(t *testing.T) {
	t.Parallel()
	ml := &mockLister{err: errors.New("ddb unavailable")}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("UP", "DOWN")
	err := h.processRecord(context.Background(), &rec)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessRecord_sqsError(t *testing.T) {
	t.Parallel()
	ml := &mockLister{
		subs: []models.Subscription{
			{GuildID: "g1", ChannelID: "c1"},
			{GuildID: "g2", ChannelID: "c2"},
		},
	}
	ms := &mockSQS{failAfter: 2}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("UP", "DOWN")
	err := h.processRecord(context.Background(), &rec)
	if err == nil {
		t.Fatal("expected error on second send")
	}
	if len(ms.bodies) != 1 {
		t.Fatalf("expected one successful send before failure, bodies=%d", len(ms.bodies))
	}
}

func TestProcessRecord_sqsImmediateError(t *testing.T) {
	t.Parallel()
	ml := &mockLister{subs: []models.Subscription{{GuildID: "g", ChannelID: "c"}}}
	ms := &mockSQS{err: errors.New("network down")}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("UP", "DOWN")
	if err := h.processRecord(context.Background(), &rec); err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessRecord_sameStatusSkipped(t *testing.T) {
	t.Parallel()
	ml := &mockLister{subs: []models.Subscription{{GuildID: "g", ChannelID: "c"}}}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("UP", "UP")
	if err := h.processRecord(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	if len(ms.bodies) != 0 {
		t.Fatalf("expected no enqueue when status unchanged")
	}
}

func TestProcessRecord_nonModifySkipped(t *testing.T) {
	t.Parallel()
	ml := &mockLister{subs: []models.Subscription{{GuildID: "g", ChannelID: "c"}}}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := statusChangeRecord("UP", "DOWN")
	rec.EventName = string(events.DynamoDBOperationTypeInsert)
	if err := h.processRecord(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	if len(ms.bodies) != 0 {
		t.Fatal("INSERT should not enqueue")
	}
}

func TestProcessRecord_missingOldStatusSkipped(t *testing.T) {
	t.Parallel()
	ml := &mockLister{subs: []models.Subscription{{GuildID: "g", ChannelID: "c"}}}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	rec := events.DynamoDBEventRecord{
		EventName: string(events.DynamoDBOperationTypeModify),
		EventID:   "evt-x",
		Change: events.DynamoDBStreamRecord{
			OldImage: map[string]events.DynamoDBAttributeValue{
				"serverId": events.NewStringAttribute("battlenet#us#11"),
			},
			NewImage: map[string]events.DynamoDBAttributeValue{
				"serverId": events.NewStringAttribute("battlenet#us#11"),
				"status":   events.NewStringAttribute("DOWN"),
			},
		},
	}
	if err := h.processRecord(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	if len(ms.bodies) != 0 {
		t.Fatal("expected skip when old status absent")
	}
}

func TestHandleRequest_multipleRecords(t *testing.T) {
	t.Parallel()
	ml := &mockLister{
		subs: []models.Subscription{{GuildID: "g", ChannelID: "c"}},
	}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	ev := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			statusChangeRecord("UP", "DOWN"),
			statusChangeRecord("DOWN", "UP"),
		},
	}
	if err := h.HandleRequest(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(ms.bodies) != 2 {
		t.Fatalf("expected 2 jobs (one per record), got %d", len(ms.bodies))
	}
}

func TestHandleRequest_stopsOnError(t *testing.T) {
	t.Parallel()
	ml := &mockLister{err: errors.New("fail")}
	ms := &mockSQS{}
	h := &Handler{list: ml, sqs: ms, queueURL: "https://sqs.example/queue"}
	ev := events.DynamoDBEvent{
		Records: []events.DynamoDBEventRecord{
			statusChangeRecord("UP", "DOWN"),
			statusChangeRecord("DOWN", "UP"),
		},
	}
	err := h.HandleRequest(context.Background(), ev)
	if err == nil {
		t.Fatal("expected error from first record")
	}
}
