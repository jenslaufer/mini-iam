package marketing

import (
	"strings"
	"testing"
)

func TestBuildMultipartEmail(t *testing.T) {
	// Test that buildMIMEMessage with an attachment produces proper multipart/mixed MIME
	// with two parts: text/html and application/pdf
	att := Attachment{
		Filename:    "guide.pdf",
		ContentType: "application/pdf",
		Data:        []byte("%PDF-test-content"),
	}
	msg := buildMIMEMessage("sender@test.com", "Sender", "to@test.com", "Test Subject", "<p>Hello</p>", map[string]string{"List-Unsubscribe": "<http://unsub>"}, []Attachment{att})

	// Should contain multipart/mixed boundary
	if !strings.Contains(string(msg), "multipart/mixed") {
		t.Error("expected multipart/mixed content type")
	}
	// Should contain HTML part
	if !strings.Contains(string(msg), "text/html") {
		t.Error("expected text/html part")
	}
	// Should contain the HTML body
	if !strings.Contains(string(msg), "<p>Hello</p>") {
		t.Error("expected HTML body in message")
	}
	// Should contain PDF attachment with base64 encoding
	if !strings.Contains(string(msg), "application/pdf") {
		t.Error("expected application/pdf part")
	}
	if !strings.Contains(string(msg), "guide.pdf") {
		t.Error("expected filename in Content-Disposition")
	}
	// Should contain base64 encoded data
	if !strings.Contains(string(msg), "base64") {
		t.Error("expected base64 encoding")
	}
	// Should contain custom header
	if !strings.Contains(string(msg), "List-Unsubscribe") {
		t.Error("expected custom headers")
	}
}

func TestBuildEmailWithoutAttachment(t *testing.T) {
	// Test that buildMIMEMessage with nil/empty attachments produces a plain text/html email
	msg := buildMIMEMessage("sender@test.com", "Sender", "to@test.com", "Test Subject", "<p>Hello</p>", nil, nil)

	// Should be plain text/html, no multipart
	if strings.Contains(string(msg), "multipart/mixed") {
		t.Error("expected no multipart for email without attachment")
	}
	if !strings.Contains(string(msg), "text/html") {
		t.Error("expected text/html content type")
	}
	if !strings.Contains(string(msg), "<p>Hello</p>") {
		t.Error("expected HTML body")
	}
}
