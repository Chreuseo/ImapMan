package api

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Chreuseo/ImapMan/internal/store"
	"github.com/go-chi/chi/v5"
)

//go:embed openapi.yaml
var docs embed.FS

type Server struct {
	Store     *store.Store
	APISecret string
}

func (s Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		data, _ := docs.ReadFile("openapi.yaml")
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(data)
	})
	r.Get("/swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://editor.swagger.io/?url="+r.Host+"/openapi.yaml", http.StatusFound)
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.requireAPISecret)
		r.Get("/mailboxes", s.listMailboxes)
		r.Post("/mailboxes", s.createMailbox)
		r.Delete("/mailboxes/{mailboxID}", s.deleteMailbox)
		r.Get("/distributors", s.listDistributors)
		r.Post("/distributors", s.createDistributor)
		r.Put("/distributors/{distributorID}", s.updateDistributor)
		r.Delete("/distributors/{distributorID}", s.deleteDistributor)
		r.Put("/mailboxes/{mailboxID}/distributor/{distributorID}", s.linkMailbox)
		r.Delete("/mailboxes/{mailboxID}/distributor/{distributorID}", s.deleteMailboxDistributor)
		r.Get("/data-sources", s.listDataSources)
		r.Post("/data-sources", s.createDataSource)
		r.Delete("/data-sources/{dataSourceID}", s.deleteDataSource)
		r.Get("/lists", s.listLists)
		r.Post("/lists", s.createList)
		r.Delete("/lists/{listID}", s.deleteList)
		r.Put("/distributors/{distributorID}/lists/{listID}", s.linkList)
		r.Delete("/distributors/{distributorID}/lists/{listID}", s.deleteDistributorList)
		r.Put("/distributors/{distributorID}/sender-lists/{listID}", s.linkSenderList)
		r.Delete("/distributors/{distributorID}/sender-lists/{listID}", s.deleteSenderList)
		r.Get("/lists/{listID}/members", s.listMembers)
		r.Post("/lists/{listID}/members", s.createMember)
		r.Delete("/lists/{listID}/members/{memberID}", s.deleteMember)
		r.Get("/processed-messages", s.processedMessages)
	})
	return r
}
func (s Server) requireAPISecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if len(authorization) < len(prefix) || authorization[:len(prefix)] != prefix ||
			subtle.ConstantTimeCompare([]byte(authorization[len(prefix):]), []byte(s.APISecret)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="ImapMan API"`)
			respond(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing API secret"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func decode(r *http.Request, v any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	return json.NewDecoder(r.Body).Decode(v)
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, err error) {
	respond(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}
func pathID(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, key), 10, 64)
}
func (s Server) listMailboxes(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.Mailboxes(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, v)
}
func (s Server) createMailbox(w http.ResponseWriter, r *http.Request) {
	var v store.Mailbox
	if e := decode(r, &v); e != nil {
		fail(w, e)
		return
	}
	v, e := s.Store.CreateMailbox(r.Context(), v)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 201, v)
}
func (s Server) deleteMailbox(w http.ResponseWriter, r *http.Request) {
	s.deleteOne(w, r, "mailboxID", s.Store.DeleteMailbox)
}
func (s Server) listDistributors(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.Distributors(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, v)
}
func (s Server) createDistributor(w http.ResponseWriter, r *http.Request) {
	var v store.Distributor
	if e := decode(r, &v); e != nil {
		fail(w, e)
		return
	}
	v, e := s.Store.CreateDistributor(r.Context(), v)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 201, v)
}
func (s Server) updateDistributor(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "distributorID")
	if e != nil {
		fail(w, e)
		return
	}
	var v store.Distributor
	if e = decode(r, &v); e != nil {
		fail(w, e)
		return
	}
	v.ID = id
	if e = s.Store.UpdateDistributor(r.Context(), v); e != nil {
		fail(w, e)
		return
	}
	respond(w, http.StatusOK, v)
}
func (s Server) deleteDistributor(w http.ResponseWriter, r *http.Request) {
	s.deleteOne(w, r, "distributorID", s.Store.DeleteDistributor)
}
func (s Server) linkMailbox(w http.ResponseWriter, r *http.Request) {
	m, e := pathID(r, "mailboxID")
	if e == nil {
		var d int64
		d, e = pathID(r, "distributorID")
		if e == nil {
			e = s.Store.LinkMailbox(r.Context(), m, d)
		}
	}
	if e != nil {
		fail(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Server) deleteMailboxDistributor(w http.ResponseWriter, r *http.Request) {
	s.deletePair(w, r, "mailboxID", "distributorID", s.Store.DeleteMailboxDistributor)
}
func (s Server) listDataSources(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.DataSources(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	for i := range v {
		v[i].DSN = ""
	}
	respond(w, 200, v)
}
func (s Server) createDataSource(w http.ResponseWriter, r *http.Request) {
	var v store.DataSource
	if e := decode(r, &v); e != nil {
		fail(w, e)
		return
	}
	v, e := s.Store.CreateDataSource(r.Context(), v)
	if e != nil {
		fail(w, e)
		return
	}
	v.DSN = ""
	respond(w, 201, v)
}
func (s Server) deleteDataSource(w http.ResponseWriter, r *http.Request) {
	s.deleteOne(w, r, "dataSourceID", s.Store.DeleteDataSource)
}
func (s Server) listLists(w http.ResponseWriter, r *http.Request) {
	v, e := s.Store.Lists(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, v)
}
func (s Server) createList(w http.ResponseWriter, r *http.Request) {
	var v store.MailingList
	if e := decode(r, &v); e != nil {
		fail(w, e)
		return
	}
	v, e := s.Store.CreateList(r.Context(), v)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 201, v)
}
func (s Server) deleteList(w http.ResponseWriter, r *http.Request) {
	s.deleteOne(w, r, "listID", s.Store.DeleteList)
}
func (s Server) linkList(w http.ResponseWriter, r *http.Request) {
	d, e := pathID(r, "distributorID")
	if e == nil {
		var l int64
		l, e = pathID(r, "listID")
		if e == nil {
			e = s.Store.LinkList(r.Context(), d, l)
		}
	}
	if e != nil {
		fail(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Server) deleteDistributorList(w http.ResponseWriter, r *http.Request) {
	s.deletePair(w, r, "distributorID", "listID", s.Store.DeleteDistributorList)
}
func (s Server) linkSenderList(w http.ResponseWriter, r *http.Request) {
	d, e := pathID(r, "distributorID")
	if e == nil {
		var l int64
		l, e = pathID(r, "listID")
		if e == nil {
			e = s.Store.LinkSenderList(r.Context(), d, l)
		}
	}
	if e != nil {
		fail(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Server) deleteSenderList(w http.ResponseWriter, r *http.Request) {
	s.deletePair(w, r, "distributorID", "listID", s.Store.DeleteSenderList)
}
func (s Server) listMembers(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "listID")
	if e != nil {
		fail(w, e)
		return
	}
	v, e := s.Store.Members(r.Context(), id)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 200, v)
}
func (s Server) createMember(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "listID")
	if e != nil {
		fail(w, e)
		return
	}
	var v store.Member
	if e = decode(r, &v); e != nil {
		fail(w, e)
		return
	}
	v.MailingListID = id
	v, e = s.Store.CreateMember(r.Context(), v)
	if e != nil {
		fail(w, e)
		return
	}
	respond(w, 201, v)
}
func (s Server) deleteMember(w http.ResponseWriter, r *http.Request) {
	listID, e := pathID(r, "listID")
	if e != nil {
		fail(w, e)
		return
	}
	memberID, e := pathID(r, "memberID")
	if e != nil {
		fail(w, e)
		return
	}
	if e = s.Store.DeleteMember(r.Context(), listID, memberID); e != nil {
		fail(w, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Server) deleteOne(w http.ResponseWriter, r *http.Request, key string, deleteFn func(context.Context, int64) error) {
	id, err := pathID(r, key)
	if err == nil {
		err = deleteFn(r.Context(), id)
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Server) deletePair(w http.ResponseWriter, r *http.Request, firstKey, secondKey string, deleteFn func(context.Context, int64, int64) error) {
	first, err := pathID(r, firstKey)
	if err == nil {
		var second int64
		second, err = pathID(r, secondKey)
		if err == nil {
			err = deleteFn(r.Context(), first, second)
		}
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s Server) processedMessages(w http.ResponseWriter, r *http.Request) {
	rows, e := s.Store.DB.QueryContext(r.Context(), `SELECT id,mailbox_id,imap_uid,message_id,subject,status,attempts,error_message,processed_at FROM processed_messages ORDER BY created_at DESC`)
	if e != nil {
		fail(w, e)
		return
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var id, mailbox, uid, attempts int64
		var messageID, subject, status string
		var errMsg, processed any
		if e = rows.Scan(&id, &mailbox, &uid, &messageID, &subject, &status, &attempts, &errMsg, &processed); e != nil {
			fail(w, e)
			return
		}
		result = append(result, map[string]any{"id": id, "mailbox_id": mailbox, "imap_uid": uid, "message_id": messageID, "subject": subject, "status": status, "attempts": attempts, "error_message": errMsg, "processed_at": processed})
	}
	respond(w, 200, result)
}
