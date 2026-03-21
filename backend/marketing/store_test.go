package marketing

import (
	"database/sql"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE contacts (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		email TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		user_id TEXT,
		unsubscribed INTEGER NOT NULL DEFAULT 0,
		unsubscribe_token TEXT UNIQUE NOT NULL,
		invite_token TEXT UNIQUE, invite_token_expires_at DATETIME,
		consent_source TEXT NOT NULL,
		consent_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		UNIQUE(tenant_id, email)
	);
	CREATE TABLE segments (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		UNIQUE(tenant_id, name)
	);
	CREATE TABLE contact_segments (
		contact_id TEXT NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (contact_id, segment_id)
	);
	CREATE TABLE campaigns (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		subject TEXT NOT NULL,
		html_body TEXT NOT NULL,
		from_name TEXT NOT NULL DEFAULT '',
		from_email TEXT NOT NULL DEFAULT '',
		attachment_url TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'draft',
		sent_at DATETIME,
		created_at DATETIME NOT NULL
	);
	CREATE TABLE campaign_segments (
		campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		segment_id TEXT NOT NULL REFERENCES segments(id) ON DELETE CASCADE,
		PRIMARY KEY (campaign_id, segment_id)
	);
	CREATE TABLE campaign_recipients (
		id TEXT PRIMARY KEY,
		campaign_id TEXT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
		contact_id TEXT NOT NULL REFERENCES contacts(id),
		status TEXT NOT NULL DEFAULT 'queued',
		error_message TEXT NOT NULL DEFAULT '',
		sent_at DATETIME,
		opened_at DATETIME,
		UNIQUE(campaign_id, contact_id)
	);
	PRAGMA foreign_keys = ON;
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(newTestDB(t))
}

// --- Contact Tests ---

func TestCreateContact(t *testing.T) {
	s := newTestStore(t)

	c, err := s.CreateContact("alice@example.com", "Alice", "api")
	if err != nil {
		t.Fatal(err)
	}
	if c.Email != "alice@example.com" {
		t.Errorf("email = %q, want alice@example.com", c.Email)
	}
	if c.Name != "Alice" {
		t.Errorf("name = %q, want Alice", c.Name)
	}
	if c.ConsentSource != "api" {
		t.Errorf("consent_source = %q, want api", c.ConsentSource)
	}
	if c.UnsubscribeToken == "" {
		t.Error("unsubscribe_token is empty")
	}
	if c.InviteToken == nil || *c.InviteToken == "" {
		t.Error("invite_token is empty")
	}
	if c.ID == "" {
		t.Error("id is empty")
	}
}

func TestCreateContactDuplicateEmail(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateContact("alice@example.com", "Alice", "api")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateContact("alice@example.com", "Alice 2", "api")
	if err == nil {
		t.Error("expected error for duplicate email")
	}
}

func TestGetContactByID(t *testing.T) {
	s := newTestStore(t)

	created, _ := s.CreateContact("bob@example.com", "Bob", "signup")
	got, err := s.GetContactByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "bob@example.com" {
		t.Errorf("email = %q, want bob@example.com", got.Email)
	}
}

func TestGetContactByIDNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.GetContactByID("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent contact")
	}
}

func TestGetContactByEmail(t *testing.T) {
	s := newTestStore(t)

	s.CreateContact("carol@example.com", "Carol", "import")
	got, err := s.GetContactByEmail("carol@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Carol" {
		t.Errorf("name = %q, want Carol", got.Name)
	}
}

func TestListContacts(t *testing.T) {
	s := newTestStore(t)

	s.CreateContact("a@example.com", "A", "api")
	s.CreateContact("b@example.com", "B", "api")

	contacts, err := s.ListContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 2 {
		t.Errorf("got %d contacts, want 2", len(contacts))
	}
}

func TestDeleteContact(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateContact("del@example.com", "Del", "api")
	if err := s.DeleteContact(c.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetContactByID(c.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteContactNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.DeleteContact("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent contact")
	}
}

func TestUnsubscribeContact(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateContact("unsub@example.com", "Unsub", "api")
	if err := s.UnsubscribeContact(c.UnsubscribeToken); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetContactByID(c.ID)
	if !got.Unsubscribed {
		t.Error("contact should be unsubscribed")
	}
}

func TestUnsubscribeContactInvalidToken(t *testing.T) {
	s := newTestStore(t)

	err := s.UnsubscribeContact("bad-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestImportContacts(t *testing.T) {
	s := newTestStore(t)

	imports := []ContactImport{
		{Email: "imp1@example.com", Name: "Imp1"},
		{Email: "imp2@example.com", Name: "Imp2"},
	}
	imported, skipped, err := s.ImportContacts(imports)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 2 {
		t.Errorf("imported = %d, want 2", imported)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}

	// Import again — should skip duplicates
	imported, skipped, err = s.ImportContacts(imports)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 0 {
		t.Errorf("imported = %d, want 0", imported)
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
}

func TestGetContactByUnsubscribeToken(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateContact("token@example.com", "Token", "api")
	got, err := s.GetContactByUnsubscribeToken(c.UnsubscribeToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != c.ID {
		t.Errorf("id = %q, want %q", got.ID, c.ID)
	}
}

func TestListContactsWithSegments(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateContact("seg@example.com", "Seg", "api")
	seg, _ := s.CreateSegment("VIP", "VIP customers")
	s.AddContactToSegment(c.ID, seg.ID)

	contacts, err := s.ListContactsWithSegments()
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 {
		t.Fatalf("got %d contacts, want 1", len(contacts))
	}
	if len(contacts[0].Segments) != 1 {
		t.Errorf("got %d segments, want 1", len(contacts[0].Segments))
	}
	if contacts[0].Segments[0].Name != "VIP" {
		t.Errorf("segment name = %q, want VIP", contacts[0].Segments[0].Name)
	}
}

// --- Segment Tests ---

func TestCreateSegment(t *testing.T) {
	s := newTestStore(t)

	seg, err := s.CreateSegment("Newsletter", "Monthly newsletter subscribers")
	if err != nil {
		t.Fatal(err)
	}
	if seg.Name != "Newsletter" {
		t.Errorf("name = %q, want Newsletter", seg.Name)
	}
	if seg.Description != "Monthly newsletter subscribers" {
		t.Errorf("description = %q", seg.Description)
	}
	if seg.ID == "" {
		t.Error("id is empty")
	}
}

func TestCreateSegmentDuplicateName(t *testing.T) {
	s := newTestStore(t)

	s.CreateSegment("Dup", "first")
	_, err := s.CreateSegment("Dup", "second")
	if err == nil {
		t.Error("expected error for duplicate segment name")
	}
}

func TestListSegments(t *testing.T) {
	s := newTestStore(t)

	s.CreateSegment("A", "")
	s.CreateSegment("B", "")

	segs, err := s.ListSegments()
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Errorf("got %d segments, want 2", len(segs))
	}
}

func TestGetSegmentByID(t *testing.T) {
	s := newTestStore(t)

	created, _ := s.CreateSegment("Get", "test")
	got, err := s.GetSegmentByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Get" {
		t.Errorf("name = %q, want Get", got.Name)
	}
}

func TestUpdateSegment(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Old", "old desc")
	updated, err := s.UpdateSegment(seg.ID, "New", "new desc")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "New" {
		t.Errorf("name = %q, want New", updated.Name)
	}
	if updated.Description != "new desc" {
		t.Errorf("description = %q, want new desc", updated.Description)
	}
}

func TestUpdateSegmentNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.UpdateSegment("nonexistent", "X", "Y")
	if err == nil {
		t.Error("expected error for nonexistent segment")
	}
}

func TestDeleteSegment(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Del", "")
	if err := s.DeleteSegment(seg.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetSegmentByID(seg.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteSegmentNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.DeleteSegment("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent segment")
	}
}

func TestAddRemoveContactToSegment(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateContact("cs@example.com", "CS", "api")
	seg, _ := s.CreateSegment("Seg", "")

	if err := s.AddContactToSegment(c.ID, seg.ID); err != nil {
		t.Fatal(err)
	}

	contacts, err := s.GetSegmentContacts(seg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 {
		t.Fatalf("got %d contacts in segment, want 1", len(contacts))
	}

	segs, err := s.GetContactSegments(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("got %d segments for contact, want 1", len(segs))
	}

	if err := s.RemoveContactFromSegment(c.ID, seg.ID); err != nil {
		t.Fatal(err)
	}

	contacts, _ = s.GetSegmentContacts(seg.ID)
	if len(contacts) != 0 {
		t.Errorf("got %d contacts after removal, want 0", len(contacts))
	}
}

func TestRemoveContactFromSegmentNotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.RemoveContactFromSegment("no-contact", "no-segment")
	if err == nil {
		t.Error("expected error when contact not in segment")
	}
}

func TestSegmentContactCount(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Counted", "")
	c1, _ := s.CreateContact("c1@example.com", "C1", "api")
	c2, _ := s.CreateContact("c2@example.com", "C2", "api")
	s.AddContactToSegment(c1.ID, seg.ID)
	s.AddContactToSegment(c2.ID, seg.ID)

	got, _ := s.GetSegmentByID(seg.ID)
	if got.ContactCount != 2 {
		t.Errorf("contact_count = %d, want 2", got.ContactCount)
	}
}

// --- Campaign Tests ---

func TestCreateCampaign(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Target", "")
	c, err := s.CreateCampaign("Welcome", "<h1>Hi</h1>", "Test", "test@example.com", "", []string{seg.ID})
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "Welcome" {
		t.Errorf("subject = %q, want Welcome", c.Subject)
	}
	if c.Status != "draft" {
		t.Errorf("status = %q, want draft", c.Status)
	}
	if len(c.SegmentIDs) != 1 {
		t.Errorf("got %d segment_ids, want 1", len(c.SegmentIDs))
	}
}

func TestListCampaigns(t *testing.T) {
	s := newTestStore(t)

	s.CreateCampaign("Camp1", "<p>1</p>", "N", "n@example.com", "", nil)
	s.CreateCampaign("Camp2", "<p>2</p>", "N", "n@example.com", "", nil)

	campaigns, err := s.ListCampaigns()
	if err != nil {
		t.Fatal(err)
	}
	if len(campaigns) != 2 {
		t.Errorf("got %d campaigns, want 2", len(campaigns))
	}
}

func TestGetCampaignByID(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("S", "")
	created, _ := s.CreateCampaign("Get", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})

	got, err := s.GetCampaignByID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Get" {
		t.Errorf("subject = %q, want Get", got.Subject)
	}
	if len(got.SegmentIDs) != 1 {
		t.Errorf("got %d segment_ids, want 1", len(got.SegmentIDs))
	}
}

func TestUpdateCampaign(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateCampaign("Old", "<p>old</p>", "N", "n@example.com", "", nil)
	updated, err := s.UpdateCampaign(c.ID, "New", "<p>new</p>", "New Name", "new@example.com", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Subject != "New" {
		t.Errorf("subject = %q, want New", updated.Subject)
	}
	if updated.FromName != "New Name" {
		t.Errorf("from_name = %q, want New Name", updated.FromName)
	}
}

func TestUpdateCampaignNotDraft(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateCampaign("Sent", "<p>x</p>", "N", "n@example.com", "", nil)
	s.SetCampaignStatus(c.ID, "sent")

	_, err := s.UpdateCampaign(c.ID, "Updated", "<p>y</p>", "N", "n@example.com", "", nil)
	if err == nil {
		t.Error("expected error when updating non-draft campaign")
	}
}

func TestDeleteCampaign(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateCampaign("Del", "<p>x</p>", "N", "n@example.com", "", nil)
	if err := s.DeleteCampaign(c.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetCampaignByID(c.ID)
	if err == nil {
		t.Error("expected error after deletion")
	}
}

func TestDeleteCampaignNotDraft(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateCampaign("Sent", "<p>x</p>", "N", "n@example.com", "", nil)
	s.SetCampaignStatus(c.ID, "sent")

	err := s.DeleteCampaign(c.ID)
	if err == nil {
		t.Error("expected error when deleting non-draft campaign")
	}
}

func TestSetCampaignStatus(t *testing.T) {
	s := newTestStore(t)

	c, _ := s.CreateCampaign("Status", "<p>x</p>", "N", "n@example.com", "", nil)

	if err := s.SetCampaignStatus(c.ID, "sending"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCampaignByID(c.ID)
	if got.Status != "sending" {
		t.Errorf("status = %q, want sending", got.Status)
	}

	if err := s.SetCampaignStatus(c.ID, "sent"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCampaignByID(c.ID)
	if got.Status != "sent" {
		t.Errorf("status = %q, want sent", got.Status)
	}
	if got.SentAt == nil {
		t.Error("sent_at should be set")
	}
}

func TestPrepareCampaignRecipients(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Prep", "")
	c1, _ := s.CreateContact("r1@example.com", "R1", "api")
	c2, _ := s.CreateContact("r2@example.com", "R2", "api")
	s.AddContactToSegment(c1.ID, seg.ID)
	s.AddContactToSegment(c2.ID, seg.ID)

	camp, _ := s.CreateCampaign("Prep", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})

	count, err := s.PrepareCampaignRecipients(camp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("prepared = %d, want 2", count)
	}

	recipients, _ := s.GetCampaignRecipients(camp.ID)
	if len(recipients) != 2 {
		t.Errorf("got %d recipients, want 2", len(recipients))
	}
	for _, r := range recipients {
		if r.Status != "queued" {
			t.Errorf("recipient status = %q, want queued", r.Status)
		}
	}
}

func TestPrepareCampaignRecipientsSkipsUnsubscribed(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Skip", "")
	c1, _ := s.CreateContact("sub@example.com", "Sub", "api")
	c2, _ := s.CreateContact("unsub@example.com", "Unsub", "api")
	s.AddContactToSegment(c1.ID, seg.ID)
	s.AddContactToSegment(c2.ID, seg.ID)
	s.UnsubscribeContact(c2.UnsubscribeToken)

	camp, _ := s.CreateCampaign("Skip", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})
	count, _ := s.PrepareCampaignRecipients(camp.ID)
	if count != 1 {
		t.Errorf("prepared = %d, want 1 (unsubscribed skipped)", count)
	}
}

func TestUpdateRecipientStatus(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Upd", "")
	c, _ := s.CreateContact("upd@example.com", "Upd", "api")
	s.AddContactToSegment(c.ID, seg.ID)
	camp, _ := s.CreateCampaign("Upd", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})
	s.PrepareCampaignRecipients(camp.ID)

	recipients, _ := s.GetCampaignRecipients(camp.ID)
	if len(recipients) == 0 {
		t.Fatal("no recipients")
	}

	s.UpdateRecipientStatus(recipients[0].ID, "sent", "")
	recipients, _ = s.GetCampaignRecipients(camp.ID)
	if recipients[0].Status != "sent" {
		t.Errorf("status = %q, want sent", recipients[0].Status)
	}
	if recipients[0].SentAt == nil {
		t.Error("sent_at should be set")
	}
}

func TestRecordOpen(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Open", "")
	c, _ := s.CreateContact("open@example.com", "Open", "api")
	s.AddContactToSegment(c.ID, seg.ID)
	camp, _ := s.CreateCampaign("Open", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})
	s.PrepareCampaignRecipients(camp.ID)

	recipients, _ := s.GetCampaignRecipients(camp.ID)
	s.RecordOpen(recipients[0].ID)

	recipients, _ = s.GetCampaignRecipients(camp.ID)
	if recipients[0].OpenedAt == nil {
		t.Error("opened_at should be set")
	}
}

func TestGetCampaignStats(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Stats", "")
	c1, _ := s.CreateContact("s1@example.com", "S1", "api")
	c2, _ := s.CreateContact("s2@example.com", "S2", "api")
	s.AddContactToSegment(c1.ID, seg.ID)
	s.AddContactToSegment(c2.ID, seg.ID)

	camp, _ := s.CreateCampaign("Stats", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})
	s.PrepareCampaignRecipients(camp.ID)

	recipients, _ := s.GetCampaignRecipients(camp.ID)
	s.UpdateRecipientStatus(recipients[0].ID, "sent", "")
	s.UpdateRecipientStatus(recipients[1].ID, "failed", "timeout")

	stats, err := s.GetCampaignStats(camp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 {
		t.Errorf("total = %d, want 2", stats.Total)
	}
	if stats.Sent != 1 {
		t.Errorf("sent = %d, want 1", stats.Sent)
	}
	if stats.Failed != 1 {
		t.Errorf("failed = %d, want 1", stats.Failed)
	}
}

// --- Campaign Sender Tests ---

func TestCampaignSenderSync(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Send", "")
	c, _ := s.CreateContact("send@example.com", "Send", "api")
	s.AddContactToSegment(c.ID, seg.ID)
	camp, _ := s.CreateCampaign("Send", "<p>Hi {{.Name}}</p>", "N", "n@example.com", "", []string{seg.ID})

	mailer := &LogMailer{}
	sender := NewCampaignSender(s, mailer, "http://localhost:8080", 0)
	sender.StartSync()
	sender.Enqueue(camp.ID, "")

	got, _ := s.GetCampaignByID(camp.ID)
	if got.Status != "sent" {
		t.Errorf("status = %q, want sent", got.Status)
	}

	recipients, _ := s.GetCampaignRecipients(camp.ID)
	if len(recipients) != 1 {
		t.Fatalf("got %d recipients, want 1", len(recipients))
	}
	if recipients[0].Status != "sent" {
		t.Errorf("recipient status = %q, want sent", recipients[0].Status)
	}
}

func TestCampaignSenderSkipsNonDraft(t *testing.T) {
	s := newTestStore(t)

	camp, _ := s.CreateCampaign("Already Sent", "<p>x</p>", "N", "n@example.com", "", nil)
	s.SetCampaignStatus(camp.ID, "sent")

	sender := NewCampaignSender(s, &LogMailer{}, "http://localhost:8080", 0)
	sender.StartSync()
	sender.Enqueue(camp.ID, "")

	got, _ := s.GetCampaignByID(camp.ID)
	if got.Status != "sent" {
		t.Errorf("status = %q, want sent (unchanged)", got.Status)
	}
}

type failMailer struct{}

func (m *failMailer) Send(to, subject, htmlBody string, headers map[string]string, attachments []Attachment) error {
	return fmt.Errorf("smtp error")
}

func TestCampaignSenderMailerFailure(t *testing.T) {
	s := newTestStore(t)

	seg, _ := s.CreateSegment("Fail", "")
	c, _ := s.CreateContact("fail@example.com", "Fail", "api")
	s.AddContactToSegment(c.ID, seg.ID)
	camp, _ := s.CreateCampaign("Fail", "<p>x</p>", "N", "n@example.com", "", []string{seg.ID})

	sender := NewCampaignSender(s, &failMailer{}, "http://localhost:8080", 0)
	sender.StartSync()
	sender.Enqueue(camp.ID, "")

	got, _ := s.GetCampaignByID(camp.ID)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}

	recipients, _ := s.GetCampaignRecipients(camp.ID)
	if len(recipients) != 1 {
		t.Fatalf("got %d recipients, want 1", len(recipients))
	}
	if recipients[0].Status != "failed" {
		t.Errorf("recipient status = %q, want failed", recipients[0].Status)
	}
}
