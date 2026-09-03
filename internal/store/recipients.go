package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

func (s *Store) RecipientsForMailbox(ctx context.Context, mailboxID int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ml.id, ml.list_type, ml.datasource_id, COALESCE(ml.recipient_table,''), COALESCE(ml.email_column,''), COALESCE(ml.filter_json,'')
		FROM mailbox_distributors md
		JOIN distributor_lists dl ON dl.distributor_id = md.distributor_id
		JOIN mailing_lists ml ON ml.id = dl.mailing_list_id
		WHERE md.mailbox_id = ?`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	for rows.Next() {
		var id int64
		var kind, table, column, rawFilter string
		var sourceID sql.NullInt64
		if err := rows.Scan(&id, &kind, &sourceID, &table, &column, &rawFilter); err != nil {
			return nil, err
		}
		emails, err := s.listEmails(ctx, id, kind, sourceID, table, column, rawFilter, true)
		if err != nil {
			return nil, err
		}
		for _, email := range emails {
			seen[email] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(seen))
	for email := range seen {
		result = append(result, email)
	}
	return result, nil
}
func (s *Store) DeliveryBatchesForMailbox(ctx context.Context, mailboxID int64, defaults DeliverySettings) ([]DeliveryBatch, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT ml.id,ml.list_type,ml.datasource_id,COALESCE(ml.recipient_table,''),COALESCE(ml.email_column,''),COALESCE(ml.filter_json,''),ml.delivery_batch_size,ml.delivery_use_bcc,ml.delivery_to_header,ml.delivery_delay_seconds FROM mailbox_distributors md JOIN distributor_lists dl ON dl.distributor_id=md.distributor_id JOIN mailing_lists ml ON ml.id=dl.mailing_list_id WHERE md.mailbox_id=?`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var batches []DeliveryBatch
	for rows.Next() {
		var id int64
		var kind, table, column, rawFilter string
		var sourceID sql.NullInt64
		var batchSize, delay sql.NullInt64
		var useBCC sql.NullBool
		var toHeader sql.NullString
		if err := rows.Scan(&id, &kind, &sourceID, &table, &column, &rawFilter, &batchSize, &useBCC, &toHeader, &delay); err != nil {
			return nil, err
		}
		settings := defaults
		if batchSize.Valid {
			settings.BatchSize = int(batchSize.Int64)
		}
		if useBCC.Valid {
			settings.UseBCC = useBCC.Bool
		}
		if toHeader.Valid {
			settings.ToHeader = toHeader.String
		}
		if delay.Valid {
			settings.DelaySeconds = int(delay.Int64)
		}
		if err := validDeliverySettings(settings); err != nil {
			return nil, fmt.Errorf("list %d: %w", id, err)
		}
		emails, err := s.listEmails(ctx, id, kind, sourceID, table, column, rawFilter, true)
		if err != nil {
			return nil, err
		}
		seen := map[string]struct{}{}
		recipients := make([]string, 0, len(emails))
		for _, email := range emails {
			normalized := strings.ToLower(strings.TrimSpace(email))
			if normalized != "" {
				if _, ok := seen[normalized]; !ok {
					seen[normalized] = struct{}{}
					recipients = append(recipients, email)
				}
			}
		}
		size := settings.BatchSize
		if !settings.UseBCC {
			size = 1
		}
		for len(recipients) > 0 {
			end := size
			if end > len(recipients) {
				end = len(recipients)
			}
			batches = append(batches, DeliveryBatch{Recipients: recipients[:end], Settings: settings})
			recipients = recipients[end:]
		}
	}
	return batches, rows.Err()
}
func (s *Store) SenderAllowed(ctx context.Context, mailboxID int64, sender string) (bool, error) {
	var distributorID int64
	var policy string
	err := s.DB.QueryRowContext(ctx, `SELECT d.id,d.sender_policy FROM mailbox_distributors md JOIN distributors d ON d.id=md.distributor_id WHERE md.mailbox_id=?`, mailboxID).Scan(&distributorID, &policy)
	if err == sql.ErrNoRows || policy == "allow_all" {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	table := "distributor_sender_lists"
	if policy == "members_only" {
		table = "distributor_lists"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT ml.id,ml.list_type,ml.datasource_id,COALESCE(ml.recipient_table,''),COALESCE(ml.email_column,''),COALESCE(ml.filter_json,'') FROM `+table+` sl JOIN mailing_lists ml ON ml.id=sl.mailing_list_id WHERE sl.distributor_id=?`, distributorID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	matched := false
	for rows.Next() {
		var id int64
		var kind, recipientTable, column, rawFilter string
		var sourceID sql.NullInt64
		if err := rows.Scan(&id, &kind, &sourceID, &recipientTable, &column, &rawFilter); err != nil {
			return false, err
		}
		emails, err := s.listEmails(ctx, id, kind, sourceID, recipientTable, column, rawFilter, false)
		if err != nil {
			return false, err
		}
		for _, email := range emails {
			if strings.EqualFold(strings.TrimSpace(email), strings.TrimSpace(sender)) {
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if policy == "blacklist" {
		return !matched, nil
	}
	return matched, nil
}
func (s *Store) listEmails(ctx context.Context, id int64, kind string, sourceID sql.NullInt64, table, column, rawFilter string, receivesMailOnly bool) ([]string, error) {
	if kind == "static" {
		return s.staticMemberEmails(ctx, id, receivesMailOnly)
	}
	if !sourceID.Valid {
		return nil, fmt.Errorf("database list %d has no data source", id)
	}
	return s.databaseRecipients(ctx, sourceID.Int64, table, column, rawFilter)
}
func (s *Store) staticRecipients(ctx context.Context, listID int64) ([]string, error) {
	return s.staticMemberEmails(ctx, listID, true)
}
func (s *Store) staticMemberEmails(ctx context.Context, listID int64, receivesMailOnly bool) ([]string, error) {
	query := `SELECT email FROM mailing_list_members WHERE mailing_list_id=?`
	if receivesMailOnly {
		query += ` AND receives_mail=TRUE`
	}
	rows, err := s.DB.QueryContext(ctx, query, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}
func (s *Store) databaseRecipients(ctx context.Context, sourceID int64, table, column, rawFilter string) ([]string, error) {
	var d DataSource
	if err := s.DB.QueryRowContext(ctx, `SELECT id,name,driver,dsn,COALESCE(username,''),COALESCE(password_env,'') FROM data_sources WHERE id=?`, sourceID).Scan(&d.ID, &d.Name, &d.Driver, &d.DSN, &d.Username, &d.PasswordEnv); err != nil {
		return nil, err
	}
	dsn := d.DSN
	if d.PasswordEnv != "" {
		password := os.Getenv(d.PasswordEnv)
		if password == "" {
			return nil, fmt.Errorf("data source %q requires unset environment variable %q", d.Name, d.PasswordEnv)
		}
		dsn = strings.ReplaceAll(dsn, "{password}", password)
	}
	dsn = strings.ReplaceAll(dsn, "{username}", d.Username)
	filter, err := decodeFilter(rawFilter)
	if err != nil {
		return nil, err
	}
	where, args, err := filterSQL(d.Driver, filter)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(normalizeDriver(d.Driver), dsn)
	if err != nil {
		return nil, fmt.Errorf("open data source %q: %w", d.Name, err)
	}
	defer db.Close()
	query := `SELECT ` + quoteIdentifier(d.Driver, column) + ` FROM ` + quoteIdentifier(d.Driver, table) + where
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

func (s *Store) BeginMessage(ctx context.Context, mailboxID, uid int64, messageID, subject string, reprocess bool, maxAttempts int) (bool, error) {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO processed_messages(mailbox_id,imap_uid,message_id,subject,status,attempts) VALUES(?,?,?,?, 'processing',1) ON CONFLICT(mailbox_id,imap_uid) DO UPDATE SET status='processing', attempts=processed_messages.attempts+1, error_message=NULL WHERE processed_messages.status='failed' AND ? AND processed_messages.attempts < ?`, mailboxID, uid, messageID, subject, reprocess, maxAttempts)
	if err != nil {
		return false, err
	}
	changed, _ := result.RowsAffected()
	return changed > 0, nil
}
func (s *Store) CompleteMessage(ctx context.Context, mailboxID, uid int64, deliveryErr error) error {
	if deliveryErr != nil {
		_, err := s.DB.ExecContext(ctx, `UPDATE processed_messages SET status='failed',error_message=?,processed_at=CURRENT_TIMESTAMP WHERE mailbox_id=? AND imap_uid=?`, deliveryErr.Error(), mailboxID, uid)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE processed_messages SET status='sent',processed_at=CURRENT_TIMESTAMP WHERE mailbox_id=? AND imap_uid=?`, mailboxID, uid)
	return err
}
