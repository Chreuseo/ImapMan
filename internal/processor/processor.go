package processor

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	"github.com/imapman/imapman/internal/config"
	"github.com/imapman/imapman/internal/store"
)

type Processor struct {
	Config config.Config
	Store  *store.Store
	Logger *slog.Logger
}

func (p Processor) Run(ctx context.Context) {
	poll := func() {
		if err := p.Poll(ctx); err != nil {
			p.Logger.Error("IMAP polling failed", "error", err)
		}
	}
	poll()
	ticker := timeTicker(p.Config.Processing.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

func (p Processor) Poll(ctx context.Context) error {
	mailboxes, err := p.Store.Mailboxes(ctx)
	if err != nil {
		return err
	}
	for _, mailbox := range mailboxes {
		password, err := config.Secret(mailbox.IMAPPasswordEnv)
		if err != nil {
			return fmt.Errorf("mailbox %q: %w", mailbox.Name, err)
		}
		address := mailbox.IMAPHost + ":" + strconv.Itoa(mailbox.IMAPPort)
		client, err := imapclient.DialTLS(address, &tls.Config{ServerName: mailbox.IMAPHost})
		if err != nil {
			return fmt.Errorf("connect mailbox %q: %w", mailbox.Name, err)
		}
		if err := client.Login(mailbox.IMAPUsername, password); err != nil {
			client.Logout()
			return fmt.Errorf("login mailbox %q: %w", mailbox.Name, err)
		}
		if _, err := client.Select(mailbox.IMAPFolder, false); err != nil {
			client.Logout()
			return fmt.Errorf("select %q: %w", mailbox.IMAPFolder, err)
		}
		if err := p.processMailbox(ctx, client, mailbox); err != nil {
			client.Logout()
			return fmt.Errorf("process %q: %w", mailbox.Name, err)
		}
		if err := client.Logout(); err != nil {
			return fmt.Errorf("logout mailbox %q: %w", mailbox.Name, err)
		}
	}
	return nil
}

func (p Processor) processMailbox(ctx context.Context, client *imapclient.Client, mailbox store.Mailbox) error {
	uids, err := client.UidSearch(&imap.SearchCriteria{WithoutFlags: []string{imap.SeenFlag}})
	if err != nil {
		return err
	}
	if len(uids) == 0 {
		return nil
	}
	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)
	section := &imap.BodySectionName{}
	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- client.UidFetch(seqset, []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, section.FetchItem()}, messages)
	}()
	for message := range messages {
		if message.Envelope == nil {
			return fmt.Errorf("UID %d has no envelope", message.Uid)
		}
		messageID := message.Envelope.MessageId
		subject := message.Envelope.Subject
		started, err := p.Store.BeginMessage(ctx, mailbox.ID, int64(message.Uid), messageID, subject, p.Config.Processing.ReprocessFailed, p.Config.Processing.MaxAttempts)
		if err != nil {
			return err
		}
		if !started {
			continue
		}
		body := message.GetBody(section)
		if body == nil {
			err = fmt.Errorf("UID %d has no RFC822 body", message.Uid)
		} else {
			sender := ""
			if len(message.Envelope.From) > 0 {
				sender = message.Envelope.From[0].MailboxName + "@" + message.Envelope.From[0].HostName
			}
			err = p.forward(ctx, mailbox, sender, body)
		}
		if completeErr := p.Store.CompleteMessage(ctx, mailbox.ID, int64(message.Uid), err); completeErr != nil {
			return completeErr
		}
		if err != nil {
			p.Logger.Error("mail delivery failed", "mailbox", mailbox.Name, "uid", message.Uid, "error", err)
			continue
		}
		if mailbox.IMAPMarkSeen {
			set := new(imap.SeqSet)
			set.AddNum(message.Uid)
			if err := client.UidStore(set, imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.SeenFlag}, nil); err != nil {
				return err
			}
		}
	}
	return <-done
}

func (p Processor) forward(ctx context.Context, mailbox store.Mailbox, sender string, body imap.Literal) error {
	allowed, err := p.Store.SenderAllowed(ctx, mailbox.ID, sender)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("sender %q is rejected by distributor policy", sender)
	}
	defaults := store.DeliverySettings{BatchSize: p.Config.Delivery.BatchSize, UseBCC: p.Config.Delivery.UseBCC, ToHeader: p.Config.Delivery.ToHeader, DelaySeconds: int(p.Config.Delivery.DelayBetween.Seconds())}
	batches, err := p.Store.DeliveryBatchesForMailbox(ctx, mailbox.ID, defaults)
	if err != nil {
		return err
	}
	if len(batches) == 0 {
		return nil
	}
	smtpConfig := p.Config.SMTP
	if mailbox.SMTPHost != "" {
		smtpConfig.Host = mailbox.SMTPHost
		smtpConfig.Port = mailbox.SMTPPort
		smtpConfig.Username = mailbox.SMTPUsername
		smtpConfig.PasswordEnv = mailbox.SMTPPasswordEnv
		smtpConfig.From = mailbox.SMTPFrom
	}
	if smtpConfig.Host == "" || smtpConfig.Port == 0 || smtpConfig.From == "" {
		return fmt.Errorf("mailbox %q has no complete SMTP configuration or central fallback", mailbox.Name)
	}
	password, err := config.Secret(smtpConfig.PasswordEnv)
	if err != nil {
		return err
	}
	address := smtpConfig.Host + ":" + strconv.Itoa(smtpConfig.Port)
	auth := smtp.PlainAuth("", smtpConfig.Username, password, smtpConfig.Host)
	data, err := readLiteral(body)
	if err != nil {
		return err
	}
	for index, batch := range batches {
		toHeader := smtpConfig.From
		if batch.Settings.ToHeader == "undisclosed" {
			toHeader = "undisclosed-recipients:;"
		}
		message := rewriteRecipientHeaders(data, toHeader)
		if err := smtp.SendMail(address, auth, smtpConfig.From, batch.Recipients, message); err != nil {
			return fmt.Errorf("SMTP delivery batch %d: %w", index+1, err)
		}
		if index < len(batches)-1 && batch.Settings.DelaySeconds > 0 {
			timer := time.NewTimer(time.Duration(batch.Settings.DelaySeconds) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil
}

func readLiteral(body imap.Literal) ([]byte, error) {
	return io.ReadAll(body)
}
func rewriteRecipientHeaders(message []byte, toHeader string) []byte {
	separator := []byte("\r\n\r\n")
	separatorText := "\r\n"
	parts := strings.SplitN(string(message), string(separator), 2)
	if len(parts) != 2 {
		separatorText = "\n"
		parts = strings.SplitN(string(message), "\n\n", 2)
	}
	if len(parts) != 2 {
		return message
	}
	var headers []string
	skip := false
	for _, line := range strings.Split(parts[0], separatorText) {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if !skip {
				headers = append(headers, line)
			}
			continue
		}
		name := strings.ToLower(strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
		skip = name == "to" || name == "cc" || name == "bcc"
		if !skip {
			headers = append(headers, line)
		}
	}
	headers = append(headers, "To: "+toHeader)
	return []byte(strings.Join(headers, separatorText) + separatorText + separatorText + parts[1])
}
