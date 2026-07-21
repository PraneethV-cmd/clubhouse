// Package server is the clubhouse coordinator: one in-memory Room guarded by a
// mutex, persisted to a JSON snapshot on every write.
package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clubhouse/internal/room"
)

// ponytail: single global lock. Fine for a clubhouse of a few people; shard by
// path only if a hundred agents ever hammer it at once.
type Server struct {
	mu        sync.Mutex
	room      room.Room
	token     string // stable room bearer
	code      string // current join code (rotates)
	prevCode  string // previous code, still valid for one rotation (grace)
	serverURL string
	snap      string
	invite    string // .clubhouse/invite path (host convenience)
}

func New(name, token, snap, serverURL string) *Server {
	s := &Server{
		token:     token,
		serverURL: serverURL,
		snap:      snap,
		code:      room.NewID(),
		invite:    filepath.Join(filepath.Dir(snap), "invite"),
		room:      room.Room{Name: name, Members: map[string]room.Member{}, Locks: map[string]room.Lock{}},
	}
	s.load()
	return s
}

// Rotate swaps in a fresh join code every d, keeping the previous one valid for
// one interval so a just-shared link still works.
func (s *Server) Rotate(d time.Duration) {
	for range time.Tick(d) {
		s.mu.Lock()
		s.prevCode, s.code = s.code, room.NewID()
		s.mu.Unlock()
		s.writeInvite()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /redeem", s.redeem) // join code -> room token (no bearer)
	mux.HandleFunc("GET /room", s.auth(s.getRoom))
	mux.HandleFunc("GET /invite", s.auth(s.getInvite))
	mux.HandleFunc("POST /join", s.auth(s.join))
	mux.HandleFunc("POST /heartbeat", s.auth(s.heartbeat))
	mux.HandleFunc("POST /lock", s.auth(s.lock))
	mux.HandleFunc("POST /unlock", s.auth(s.unlock))
	mux.HandleFunc("POST /blocked", s.auth(s.blocked))
	mux.HandleFunc("POST /memory", s.auth(s.memory))
	return mux
}

func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.SweepOnce()
	s.writeInvite()
	go s.Rotate(rotateEvery)
	go s.sweep()
	return http.Serve(ln, s.Handler())
}

var (
	rotateEvery        = 10 * time.Minute
	lockTTL            = 2 * time.Minute  // a lock not refreshed by an edit expires
	memberTTL          = 60 * time.Second // a member with no heartbeat is pruned
	sweepEvery         = 10 * time.Second
	maxBodyBytes int64 = 1 << 20
)

// SetRotate overrides the code rotation interval (call before ListenAndServe).
func SetRotate(d time.Duration) {
	if d > 0 {
		rotateEvery = d
	}
}

func SetLeaseDurations(member, lock, sweep time.Duration) {
	if member > 0 {
		memberTTL = member
	}
	if lock > 0 {
		lockTTL = lock
	}
	if sweep > 0 {
		sweepEvery = sweep
	}
}

// sweep releases stale locks (holder crashed / idle) and prunes ghost members,
// so nothing leaks when a Codex session dies without a clean Stop hook.
func (s *Server) sweep() {
	for range time.Tick(sweepEvery) {
		s.SweepOnce()
	}
}

func (s *Server) SweepOnce() {
	s.mu.Lock()
	now := time.Now()
	for path, l := range s.room.Locks {
		if _, ok := s.room.Members[l.Member]; !ok {
			delete(s.room.Locks, path)
			s.emit("released", "someone", path+" (orphaned)")
			continue
		}
		if now.Sub(l.Since) > lockTTL {
			delete(s.room.Locks, path)
			s.emit("released", s.nameOf(l.Member), path+" (expired)")
		}
	}
	for id, m := range s.room.Members {
		if now.Sub(m.LastSeen) > memberTTL {
			s.releaseMemberLocks(id, "member left")
			delete(s.room.Members, id)
		}
	}
	s.save()
	s.mu.Unlock()
}

const maxEvents = 60

// emit appends to the activity feed. Caller must hold s.mu.
func (s *Server) emit(kind, actor, detail string) {
	s.room.Events = append(s.room.Events, room.Event{At: time.Now(), Kind: kind, Actor: actor, Detail: detail})
	if len(s.room.Events) > maxEvents {
		s.room.Events = s.room.Events[len(s.room.Events)-maxEvents:]
	}
}

func (s *Server) nameOf(id string) string {
	if m, ok := s.room.Members[id]; ok && m.Name != "" {
		return m.Name
	}
	return "someone"
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func (s *Server) redeem(w http.ResponseWriter, r *http.Request) {
	var req room.RedeemReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	ok := req.Code != "" && (req.Code == s.code || req.Code == s.prevCode)
	token := s.token
	s.mu.Unlock()
	if !ok {
		http.Error(w, "invite expired — ask for a fresh link", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, room.RedeemResp{Token: token})
}

func (s *Server) getInvite(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	link := room.MakeInvite(s.serverURL, s.code)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, room.InviteResp{Link: link})
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	snap := s.cloneRoom()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req room.JoinReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	id := room.NewID()
	s.room.Members[id] = room.Member{ID: id, Name: req.Name, LastSeen: time.Now()}
	s.emit("joined", req.Name, "")
	snap := s.cloneRoom()
	s.save()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, room.JoinResp{MemberID: id, Room: snap})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var req room.BeatReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	if m, ok := s.room.Members[req.MemberID]; ok {
		m.LastSeen = time.Now()
		m.Status = req.Status
		m.Git = req.Git
		s.room.Members[req.MemberID] = m
	}
	snap := s.cloneRoom()
	s.save()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) lock(w http.ResponseWriter, r *http.Request) {
	var req room.LockReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.requireMember(w, req.MemberID) {
		return
	}
	prev, existed := s.room.Locks[req.Path]
	if existed && prev.Member != req.MemberID {
		s.emit("blocked", s.nameOf(req.MemberID), req.Path)
		writeJSON(w, http.StatusConflict, prev)
		return
	}
	s.room.Locks[req.Path] = room.Lock{Path: req.Path, Member: req.MemberID, Reason: req.Reason, Since: time.Now()}
	if !existed {
		s.emit("locked", s.nameOf(req.MemberID), req.Path)
	}
	s.save()
	writeJSON(w, http.StatusOK, s.cloneRoom())
}

func (s *Server) unlock(w http.ResponseWriter, r *http.Request) {
	var req room.LockReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	if !s.requireMember(w, req.MemberID) {
		s.mu.Unlock()
		return
	}
	if held, ok := s.room.Locks[req.Path]; ok && held.Member == req.MemberID {
		delete(s.room.Locks, req.Path)
		s.emit("released", s.nameOf(req.MemberID), req.Path)
		s.save()
	}
	snap := s.cloneRoom()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, snap)
}

// releaseMemberLocks releases every lock held by a member. Caller must hold
// s.mu. This is the lease safety net for crashes, network partitions, and
// laptops going to sleep without a Stop hook.
func (s *Server) releaseMemberLocks(memberID, reason string) {
	for path, held := range s.room.Locks {
		if held.Member == memberID {
			delete(s.room.Locks, path)
			detail := path
			if reason != "" {
				detail += " (" + reason + ")"
			}
			s.emit("released", s.nameOf(memberID), detail)
		}
	}
}

func (s *Server) memory(w http.ResponseWriter, r *http.Request) {
	var req room.NoteReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	if !s.requireMember(w, req.MemberID) {
		s.mu.Unlock()
		return
	}
	author := req.MemberID
	if m, ok := s.room.Members[req.MemberID]; ok {
		author = m.Name
	}
	s.room.Notes = append(s.room.Notes, room.Note{Author: author, Text: req.Text, At: time.Now()})
	snap := s.cloneRoom()
	s.save()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, snap)
}

// blocked records a client-side denial (e.g. a shell write) in the feed.
func (s *Server) blocked(w http.ResponseWriter, r *http.Request) {
	var req room.LockReq
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	if !s.requireMember(w, req.MemberID) {
		s.mu.Unlock()
		return
	}
	s.emit("blocked", s.nameOf(req.MemberID), req.Path)
	s.save()
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, struct{}{})
}

// requireMember rejects mutating operations from stale session ids. Heartbeat
// stays lenient so clients can detect a pruned session and rejoin cleanly.
// Caller must hold s.mu.
func (s *Server) requireMember(w http.ResponseWriter, id string) bool {
	if id == "" {
		http.Error(w, "join required", http.StatusPreconditionRequired)
		return false
	}
	if _, ok := s.room.Members[id]; !ok {
		http.Error(w, "member session expired - rejoin clubhouse", http.StatusPreconditionRequired)
		return false
	}
	return true
}

// cloneRoom returns a copy safe to marshal after the lock is released.
// Caller must hold s.mu.
func (s *Server) cloneRoom() room.Room {
	r := room.Room{Name: s.room.Name, Members: map[string]room.Member{}, Locks: map[string]room.Lock{}}
	for k, v := range s.room.Members {
		r.Members[k] = v
	}
	for k, v := range s.room.Locks {
		r.Locks[k] = v
	}
	r.Notes = append(r.Notes, s.room.Notes...)
	r.Events = append(r.Events, s.room.Events...)
	return r
}

// writeInvite drops the current link on disk so `clubhouse invite` works locally.
func (s *Server) writeInvite() {
	s.mu.Lock()
	link := room.MakeInvite(s.serverURL, s.code)
	s.mu.Unlock()
	os.MkdirAll(filepath.Dir(s.invite), 0o755)
	os.WriteFile(s.invite, []byte(link), 0o644)
}

// save/load persist the snapshot. Caller must hold s.mu.
func (s *Server) save() {
	if s.snap == "" {
		return
	}
	if b, err := json.MarshalIndent(s.room, "", "  "); err == nil {
		writeAtomic(s.snap, b, 0o644)
	}
}

func (s *Server) load() {
	b, err := os.ReadFile(s.snap)
	if err != nil {
		return
	}
	json.Unmarshal(b, &s.room)
	if s.room.Members == nil {
		s.room.Members = map[string]room.Member{}
	}
	if s.room.Locks == nil {
		s.room.Locks = map[string]room.Lock{}
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "request body must contain one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeAtomic(path string, body []byte, mode os.FileMode) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".clubhouse-*.tmp")
	if err != nil {
		return
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(body); err != nil {
		f.Close()
		return
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return
	}
	if err := f.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err == nil {
		os.Remove(tmp)
	}
}
