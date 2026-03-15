package main

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// --- Models ---

type Contact struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	Name             string    `json:"name"`
	UserID           *string   `json:"user_id,omitempty"`
	Unsubscribed     bool      `json:"unsubscribed"`
	UnsubscribeToken string    `json:"-"`
	ConsentSource    string    `json:"consent_source"`
	ConsentAt        time.Time `json:"consent_at"`
	CreatedAt        time.Time `json:"created_at"`
	Segments         []Segment `json:"segments,omitempty"`
}

type ContactImport struct {
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Segments []string `json:"segments,omitempty"`
}

// --- Store methods ---

func (s *Store) CreateContact(email, name, consentSource string) (*Contact, error) {
	c := &Contact{
		ID:               uuid.NewString(),
		Email:            email,
		Name:             name,
		UnsubscribeToken: uuid.NewString(),
		ConsentSource:    consentSource,
		ConsentAt:        time.Now().UTC(),
		CreatedAt:        time.Now().UTC(),
	}

	_, err := s.db.Exec(
		`INSERT INTO contacts (id, email, name, unsubscribe_token, consent_source, consent_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Email, c.Name, c.UnsubscribeToken, c.ConsentSource, c.ConsentAt, c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) GetContactByID(id string) (*Contact, error) {
	c := &Contact{}
	var userID sql.NullString
	var unsub int
	err := s.db.QueryRow(
		`SELECT id, email, name, user_id, unsubscribed, unsubscribe_token, consent_source, consent_at, created_at
		 FROM contacts WHERE id = ?`, id,
	).Scan(&c.ID, &c.Email, &c.Name, &userID, &unsub, &c.UnsubscribeToken, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Unsubscribed = unsub != 0
	if userID.Valid {
		c.UserID = &userID.String
	}
	return c, nil
}

func (s *Store) GetContactByEmail(email string) (*Contact, error) {
	c := &Contact{}
	var userID sql.NullString
	var unsub int
	err := s.db.QueryRow(
		`SELECT id, email, name, user_id, unsubscribed, unsubscribe_token, consent_source, consent_at, created_at
		 FROM contacts WHERE email = ?`, email,
	).Scan(&c.ID, &c.Email, &c.Name, &userID, &unsub, &c.UnsubscribeToken, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Unsubscribed = unsub != 0
	if userID.Valid {
		c.UserID = &userID.String
	}
	return c, nil
}

func (s *Store) ListContacts() ([]Contact, error) {
	rows, err := s.db.Query(
		`SELECT id, email, name, user_id, unsubscribed, consent_source, consent_at, created_at
		 FROM contacts ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		var userID sql.NullString
		var unsub int
		if err := rows.Scan(&c.ID, &c.Email, &c.Name, &userID, &unsub, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Unsubscribed = unsub != 0
		if userID.Valid {
			c.UserID = &userID.String
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

func (s *Store) DeleteContact(id string) error {
	result, err := s.db.Exec("DELETE FROM contacts WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("contact not found")
	}
	return nil
}

func (s *Store) UnsubscribeContact(token string) error {
	result, err := s.db.Exec("UPDATE contacts SET unsubscribed = 1 WHERE unsubscribe_token = ?", token)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("invalid unsubscribe token")
	}
	return nil
}

func (s *Store) ImportContacts(contacts []ContactImport) (imported int, skipped int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO contacts (id, email, name, unsubscribe_token, consent_source, consent_at, created_at)
		 VALUES (?, ?, ?, ?, 'import', ?, ?)`,
	)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, c := range contacts {
		result, err := stmt.Exec(uuid.NewString(), c.Email, c.Name, uuid.NewString(), now, now)
		if err != nil {
			return imported, skipped, err
		}
		rows, _ := result.RowsAffected()
		if rows > 0 {
			imported++
		} else {
			skipped++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return imported, skipped, nil
}

func (s *Store) LinkContactToUser(contactID, userID string) error {
	result, err := s.db.Exec("UPDATE contacts SET user_id = ? WHERE id = ?", userID, contactID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("contact not found")
	}
	return nil
}

func (s *Store) GetContactByUnsubscribeToken(token string) (*Contact, error) {
	c := &Contact{}
	var userID sql.NullString
	var unsub int
	err := s.db.QueryRow(
		`SELECT id, email, name, user_id, unsubscribed, unsubscribe_token, consent_source, consent_at, created_at
		 FROM contacts WHERE unsubscribe_token = ?`, token,
	).Scan(&c.ID, &c.Email, &c.Name, &userID, &unsub, &c.UnsubscribeToken, &c.ConsentSource, &c.ConsentAt, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Unsubscribed = unsub != 0
	if userID.Valid {
		c.UserID = &userID.String
	}
	return c, nil
}

// ListContactsWithSegments returns all contacts with their segments populated.
func (s *Store) ListContactsWithSegments() ([]Contact, error) {
	contacts, err := s.ListContacts()
	if err != nil {
		return nil, err
	}
	for i := range contacts {
		segs, err := s.GetContactSegments(contacts[i].ID)
		if err != nil {
			return nil, err
		}
		contacts[i].Segments = segs
	}
	return contacts, nil
}
