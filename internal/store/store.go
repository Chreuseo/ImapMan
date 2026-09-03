package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaFS embed.FS

type Store struct{ DB *sql.DB }

type Mailbox struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	IMAPHost        string `json:"imap_host"`
	IMAPPort        int    `json:"imap_port"`
	IMAPUsername    string `json:"imap_username"`
	IMAPPasswordEnv string `json:"imap_password_env"`
	IMAPFolder      string `json:"imap_folder"`
	IMAPMarkSeen    bool   `json:"imap_mark_seen"`
	SMTPHost        string `json:"smtp_host,omitempty"`
	SMTPPort        int    `json:"smtp_port,omitempty"`
	SMTPUsername    string `json:"smtp_username,omitempty"`
	SMTPPasswordEnv string `json:"smtp_password_env,omitempty"`
	SMTPFrom        string `json:"smtp_from,omitempty"`
}
type Distributor struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	SenderPolicy string `json:"sender_policy"`
}
type DataSource struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Driver      string `json:"driver"`
	DSN         string `json:"dsn,omitempty"`
	Username    string `json:"username,omitempty"`
	PasswordEnv string `json:"password_env,omitempty"`
}
type MailingList struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	ListType       string            `json:"list_type"`
	DataSourceID   *int64            `json:"datasource_id,omitempty"`
	RecipientTable string            `json:"recipient_table,omitempty"`
	EmailColumn    string            `json:"email_column,omitempty"`
	NameColumn     string            `json:"name_column,omitempty"`
	Filter         []Condition       `json:"filter,omitempty"`
	Delivery       *DeliverySettings `json:"delivery,omitempty"`
}
type DeliverySettings struct {
	BatchSize    int    `json:"batch_size"`
	UseBCC       bool   `json:"use_bcc"`
	ToHeader     string `json:"to_header"`
	DelaySeconds int    `json:"delay_seconds"`
}
type DeliveryBatch struct {
	Recipients []string
	Settings   DeliverySettings
}
type Member struct {
	ID            int64  `json:"id"`
	MailingListID int64  `json:"mailing_list_id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	ReceivesMail  bool   `json:"receives_mail"`
	MemberSince   string `json:"member_since"`
}
type Condition struct {
	Column string `json:"column"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
}

func Open(ctx context.Context, driver, dsn string) (*Store, error) {
	driver = normalizeDriver(driver)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	return &Store{DB: db}, nil
}

func normalizeDriver(driver string) string {
	return driver
}

func (s *Store) CreateMailbox(ctx context.Context, m Mailbox) (Mailbox, error) {
	if m.IMAPPort == 0 {
		m.IMAPPort = 993
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO mailboxes(name,imap_host,imap_port,imap_username,imap_password_env,imap_folder,imap_mark_seen,smtp_host,smtp_port,smtp_username,smtp_password_env,smtp_from) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, m.Name, m.IMAPHost, m.IMAPPort, m.IMAPUsername, m.IMAPPasswordEnv, m.IMAPFolder, m.IMAPMarkSeen, nullIfEmpty(m.SMTPHost), nullIfZero(m.SMTPPort), nullIfEmpty(m.SMTPUsername), nullIfEmpty(m.SMTPPasswordEnv), nullIfEmpty(m.SMTPFrom))
	if err != nil {
		return m, err
	}
	m.ID, _ = result.LastInsertId()
	return m, nil
}
func (s *Store) Mailboxes(ctx context.Context) ([]Mailbox, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,imap_host,imap_port,imap_username,imap_password_env,imap_folder,imap_mark_seen,COALESCE(smtp_host,''),COALESCE(smtp_port,0),COALESCE(smtp_username,''),COALESCE(smtp_password_env,''),COALESCE(smtp_from,'') FROM mailboxes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Mailbox
	for rows.Next() {
		var m Mailbox
		if err := rows.Scan(&m.ID, &m.Name, &m.IMAPHost, &m.IMAPPort, &m.IMAPUsername, &m.IMAPPasswordEnv, &m.IMAPFolder, &m.IMAPMarkSeen, &m.SMTPHost, &m.SMTPPort, &m.SMTPUsername, &m.SMTPPasswordEnv, &m.SMTPFrom); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s *Store) CreateDistributor(ctx context.Context, d Distributor) (Distributor, error) {
	if d.SenderPolicy == "" {
		d.SenderPolicy = "allow_all"
	}
	if !validSenderPolicy(d.SenderPolicy) {
		return d, fmt.Errorf("invalid sender_policy %q", d.SenderPolicy)
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO distributors(name,sender_policy) VALUES(?,?)`, d.Name, d.SenderPolicy)
	if err != nil {
		return d, err
	}
	d.ID, _ = result.LastInsertId()
	return d, nil
}
func (s *Store) Distributors(ctx context.Context) ([]Distributor, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,sender_policy FROM distributors ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Distributor
	for rows.Next() {
		var d Distributor
		if err := rows.Scan(&d.ID, &d.Name, &d.SenderPolicy); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (s *Store) UpdateDistributor(ctx context.Context, d Distributor) error {
	if !validSenderPolicy(d.SenderPolicy) {
		return fmt.Errorf("invalid sender_policy %q", d.SenderPolicy)
	}
	result, err := s.DB.ExecContext(ctx, `UPDATE distributors SET name=?,sender_policy=? WHERE id=?`, d.Name, d.SenderPolicy, d.ID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("distributor %d not found", d.ID)
	}
	return nil
}
func (s *Store) LinkMailbox(ctx context.Context, mailboxID, distributorID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO mailbox_distributors(mailbox_id, distributor_id) VALUES(?, ?) ON DUPLICATE KEY UPDATE distributor_id=VALUES(distributor_id)`, mailboxID, distributorID)
	return err
}
func (s *Store) LinkList(ctx context.Context, distributorID, listID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO distributor_lists(distributor_id, mailing_list_id) VALUES(?, ?)`, distributorID, listID)
	return err
}
func (s *Store) LinkSenderList(ctx context.Context, distributorID, listID int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO distributor_sender_lists(distributor_id, mailing_list_id) VALUES(?, ?)`, distributorID, listID)
	return err
}
func (s *Store) DeleteMailbox(ctx context.Context, id int64) error {
	return s.deleteOne(ctx, `DELETE FROM mailboxes WHERE id=?`, id, "mailbox")
}
func (s *Store) DeleteDistributor(ctx context.Context, id int64) error {
	return s.deleteOne(ctx, `DELETE FROM distributors WHERE id=?`, id, "distributor")
}
func (s *Store) DeleteDataSource(ctx context.Context, id int64) error {
	return s.deleteOne(ctx, `DELETE FROM data_sources WHERE id=?`, id, "data source")
}
func (s *Store) DeleteList(ctx context.Context, id int64) error {
	return s.deleteOne(ctx, `DELETE FROM mailing_lists WHERE id=?`, id, "mailing list")
}
func (s *Store) DeleteMailboxDistributor(ctx context.Context, mailboxID, distributorID int64) error {
	return s.deleteOne(ctx, `DELETE FROM mailbox_distributors WHERE mailbox_id=? AND distributor_id=?`, mailboxID, distributorID, "mailbox distributor assignment")
}
func (s *Store) DeleteDistributorList(ctx context.Context, distributorID, listID int64) error {
	return s.deleteOne(ctx, `DELETE FROM distributor_lists WHERE distributor_id=? AND mailing_list_id=?`, distributorID, listID, "distributor mailing list assignment")
}
func (s *Store) DeleteSenderList(ctx context.Context, distributorID, listID int64) error {
	return s.deleteOne(ctx, `DELETE FROM distributor_sender_lists WHERE distributor_id=? AND mailing_list_id=?`, distributorID, listID, "distributor sender list assignment")
}
func (s *Store) CreateDataSource(ctx context.Context, d DataSource) (DataSource, error) {
	if !validExternalDriver(d.Driver) {
		return d, fmt.Errorf("unsupported driver %q", d.Driver)
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO data_sources(name, driver, dsn, username, password_env) VALUES(?, ?, ?, ?, ?)`, d.Name, d.Driver, d.DSN, d.Username, d.PasswordEnv)
	if err != nil {
		return d, err
	}
	d.ID, _ = result.LastInsertId()
	return d, nil
}
func (s *Store) DataSources(ctx context.Context) ([]DataSource, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, driver, dsn, COALESCE(username,''), COALESCE(password_env,'') FROM data_sources ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []DataSource
	for rows.Next() {
		var d DataSource
		if err := rows.Scan(&d.ID, &d.Name, &d.Driver, &d.DSN, &d.Username, &d.PasswordEnv); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (s *Store) CreateList(ctx context.Context, l MailingList) (MailingList, error) {
	if l.ListType != "static" && l.ListType != "database" {
		return l, fmt.Errorf("list_type must be static or database")
	}
	if l.ListType == "database" && (l.DataSourceID == nil || !safeIdentifier(l.RecipientTable) || !safeIdentifier(l.EmailColumn)) {
		return l, fmt.Errorf("database lists require datasource_id and valid recipient_table/email_column")
	}
	filter, err := encodeFilter(l.Filter)
	if err != nil {
		return l, err
	}
	var batchSize, useBCC, toHeader, delay any
	if l.Delivery != nil {
		if err := validDeliverySettings(*l.Delivery); err != nil {
			return l, err
		}
		batchSize = l.Delivery.BatchSize
		useBCC = l.Delivery.UseBCC
		toHeader = l.Delivery.ToHeader
		delay = l.Delivery.DelaySeconds
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO mailing_lists(name,list_type,datasource_id,recipient_table,email_column,name_column,filter_json,delivery_batch_size,delivery_use_bcc,delivery_to_header,delivery_delay_seconds) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, l.Name, l.ListType, l.DataSourceID, l.RecipientTable, l.EmailColumn, l.NameColumn, filter, batchSize, useBCC, toHeader, delay)
	if err != nil {
		return l, err
	}
	l.ID, _ = result.LastInsertId()
	return l, nil
}
func (s *Store) Lists(ctx context.Context) ([]MailingList, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,list_type,datasource_id,COALESCE(recipient_table,''),COALESCE(email_column,''),COALESCE(name_column,''),COALESCE(filter_json,''),delivery_batch_size,delivery_use_bcc,delivery_to_header,delivery_delay_seconds FROM mailing_lists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MailingList
	for rows.Next() {
		var l MailingList
		var filter string
		var batchSize, delay sql.NullInt64
		var useBCC sql.NullBool
		var toHeader sql.NullString
		if err := rows.Scan(&l.ID, &l.Name, &l.ListType, &l.DataSourceID, &l.RecipientTable, &l.EmailColumn, &l.NameColumn, &filter, &batchSize, &useBCC, &toHeader, &delay); err != nil {
			return nil, err
		}
		l.Filter, _ = decodeFilter(filter)
		if batchSize.Valid || useBCC.Valid || toHeader.Valid || delay.Valid {
			l.Delivery = &DeliverySettings{BatchSize: int(batchSize.Int64), UseBCC: useBCC.Bool, ToHeader: toHeader.String, DelaySeconds: int(delay.Int64)}
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
func (s *Store) CreateMember(ctx context.Context, m Member) (Member, error) {
	result, err := s.DB.ExecContext(ctx, `INSERT INTO mailing_list_members(mailing_list_id,name,email,receives_mail) VALUES(?,?,?,?)`, m.MailingListID, m.Name, m.Email, m.ReceivesMail)
	if err != nil {
		return m, err
	}
	m.ID, _ = result.LastInsertId()
	return m, nil
}
func (s *Store) Members(ctx context.Context, listID int64) ([]Member, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,mailing_list_id,name,email,receives_mail,member_since FROM mailing_list_members WHERE mailing_list_id=? ORDER BY email`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.MailingListID, &m.Name, &m.Email, &m.ReceivesMail, &m.MemberSince); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s *Store) DeleteMember(ctx context.Context, listID, memberID int64) error {
	return s.deleteOne(ctx, `DELETE FROM mailing_list_members WHERE id=? AND mailing_list_id=?`, memberID, listID, "member")
}
func (s *Store) deleteOne(ctx context.Context, query string, args ...any) error {
	label := args[len(args)-1].(string)
	args = args[:len(args)-1]
	result, err := s.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%s not found", label)
	}
	return nil
}
func safeIdentifier(v string) bool {
	if v == "" {
		return false
	}
	for i, r := range v {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
func validExternalDriver(d string) bool { return d == "postgres" || d == "mysql" || d == "sqlite" }
func validSenderPolicy(policy string) bool {
	return policy == "allow_all" || policy == "whitelist" || policy == "blacklist" || policy == "members_only"
}
func validDeliverySettings(settings DeliverySettings) error {
	if settings.BatchSize < 1 {
		return fmt.Errorf("delivery.batch_size must be at least 1")
	}
	if settings.ToHeader != "from" && settings.ToHeader != "undisclosed" {
		return fmt.Errorf("delivery.to_header must be from or undisclosed")
	}
	if settings.DelaySeconds < 0 {
		return fmt.Errorf("delivery.delay_seconds cannot be negative")
	}
	return nil
}
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullIfZero(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
func quoteIdentifier(driver, identifier string) string {
	if driver == "mysql" {
		return "`" + identifier + "`"
	}
	return `"` + identifier + `"`
}
func placeholders(driver string, count int) []string {
	p := make([]string, count)
	for i := range p {
		if driver == "postgres" {
			p[i] = fmt.Sprintf("$%d", i+1)
		} else {
			p[i] = "?"
		}
	}
	return p
}
func filterSQL(driver string, conditions []Condition) (string, []any, error) {
	parts := make([]string, 0, len(conditions))
	args := make([]any, 0, len(conditions))
	for _, c := range conditions {
		if !safeIdentifier(c.Column) || !(c.Op == "=" || c.Op == "!=" || c.Op == "<" || c.Op == "<=" || c.Op == ">" || c.Op == ">=" || c.Op == "LIKE") {
			return "", nil, fmt.Errorf("invalid filter condition")
		}
		args = append(args, c.Value)
		parts = append(parts, quoteIdentifier(driver, c.Column)+" "+c.Op+" "+placeholders(driver, len(args))[len(args)-1])
	}
	if len(parts) == 0 {
		return "", args, nil
	}
	return " WHERE " + strings.Join(parts, " AND "), args, nil
}
