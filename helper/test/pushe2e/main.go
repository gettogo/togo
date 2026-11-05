// Command pushe2e drives the push path end to end for scripts/e2e-push.sh:
// it pairs a simulator's device keys with a helper, runs a local stand-in
// for the relay, opens a `togo ask` approval and waits for its answer, and
// seals pushes for a device directly.
//
// It never prints keys, tokens, ciphertexts or plaintexts; files it writes
// stay in the directories it is given.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "pair":
		err = runPair(ctx, os.Args[2:])
	case "relay":
		err = runRelay(ctx, os.Args[2:])
	case "ask":
		err = runAsk(ctx, os.Args[2:])
	case "seal":
		err = runSeal(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pushe2e %s: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pushe2e pair|relay|ask|seal [flags]")
	os.Exit(2)
}

// readDevice reads the push.pair params the app wrote for its device.
func readDevice(path string) (wire.PairParams, error) {
	var p wire.PairParams
	raw, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, errors.New("device file is not push.pair params")
	}
	return p, nil
}

// pairDevice pairs p with the helper behind togo and returns its answer.
func pairDevice(ctx context.Context, togo string, env []string, p wire.PairParams) (wire.PairResult, error) {
	var res wire.PairResult
	h, err := dial(ctx, togo, env)
	if err != nil {
		return res, err
	}
	defer h.close()
	err = h.call(wire.MethodPushPair, p, &res)
	return res, err
}

func runPair(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	togo := fs.String("togo", "togo", "helper binary")
	device := fs.String("device", "", "push.pair params written by the app")
	relay := fs.String("relay", "", "relay URL to pair with, replacing the device's")
	out := fs.String("out", "", "where to write the pairing for the app")
	fs.Parse(args)
	p, err := readDevice(*device)
	if err != nil {
		return err
	}
	if *relay != "" {
		p.RelayURL = *relay
	}
	res, err := pairDevice(ctx, *togo, os.Environ(), p)
	if err != nil {
		return err
	}
	// What the app stores as its Pairing: the helper's answer and the relay.
	raw, err := json.Marshal(map[string]string{"hostId": res.HostID, "hostKey": res.HostKey, "relayUrl": p.RelayURL})
	if err != nil {
		return err
	}
	return os.WriteFile(*out, raw, 0o600)
}

func runRelay(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("relay", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:0", "address to listen on")
	dir := fs.String("dir", "", "directory for .apns files and relay.json")
	addrFile := fs.String("addr-file", "", "where to write the relay URL once listening")
	forbid := fs.String("forbid", "", "|-separated strings no request body may contain")
	fs.Parse(args)
	if *dir == "" || *addrFile == "" {
		return errors.New("-dir and -addr-file are required")
	}
	r := newLocalRelay(*dir, strings.Split(*forbid, "|"))
	r.onChange = func() { writeRelayStatus(r) }
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := os.WriteFile(*addrFile, []byte("http://"+ln.Addr().String()), 0o600); err != nil {
		return err
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// relayStatus is relay.json: what the script asserts on.
type relayStatus struct {
	Pushes        []relayStatusPush `json:"pushes"`
	Plaintext     bool              `json:"plaintext"`
	MailboxPosted int               `json:"mailboxPosted"`
	MailboxAcked  int               `json:"mailboxAcked"`
}

type relayStatusPush struct {
	Kind string `json:"kind"`
	File string `json:"file,omitempty"`
}

// writeRelayStatus runs with r.mu held.
func writeRelayStatus(r *localRelay) {
	st := relayStatus{Plaintext: len(r.plaintext) > 0, MailboxPosted: r.posted, MailboxAcked: r.acked, Pushes: []relayStatusPush{}}
	for _, p := range r.pushes {
		st.Pushes = append(st.Pushes, relayStatusPush{Kind: p.Kind, File: p.File})
	}
	raw, _ := json.Marshal(st)
	tmp := filepath.Join(r.dir, ".relay.json")
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(r.dir, "relay.json"))
	}
}

func runAsk(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	togo := fs.String("togo", "togo", "helper binary")
	state := fs.String("state", "", "directory for request-id, ask-exit, ask-out and closed")
	question := fs.String("question", "", "the question")
	options := fs.String("options", "", "comma-separated answers")
	timeout := fs.Int("timeout", 120, "seconds to wait for an answer")
	fs.Parse(args)
	if *state == "" || *question == "" {
		return errors.New("-state and -question are required")
	}
	h, err := dial(ctx, *togo, os.Environ())
	if err != nil {
		return err
	}
	defer h.close()
	if err := h.subscribe(); err != nil {
		return err
	}
	argv := []string{"ask", *question, "--timeout", strconv.Itoa(*timeout), "--require-remote"}
	if *options != "" {
		argv = append(argv, "--options", *options)
	}
	a, err := startAsk(ctx, *togo, os.Environ(), argv)
	if err != nil {
		return err
	}
	open, err := h.next(wire.EventApprovalOpen, 20*time.Second)
	if err != nil {
		return err
	}
	id := open.Approval.RequestID
	if err := os.WriteFile(filepath.Join(*state, "request-id"), []byte(id), 0o600); err != nil {
		return err
	}
	code, out := a.wait()
	_ = os.WriteFile(filepath.Join(*state, "ask-out"), []byte(out), 0o600)
	_ = os.WriteFile(filepath.Join(*state, "ask-exit"), []byte(strconv.Itoa(code)), 0o600)
	for {
		ev, err := h.next(wire.EventApprovalClosed, 10*time.Second)
		if err != nil {
			return err
		}
		if ev.RequestID == id {
			return os.WriteFile(filepath.Join(*state, "closed"), []byte(id), 0o600)
		}
	}
}

// asked is a running `togo ask`.
type asked struct {
	cmd *exec.Cmd
	out *strings.Builder
}

func startAsk(ctx context.Context, togo string, env, argv []string) (*asked, error) {
	cmd := exec.CommandContext(ctx, togo, argv...)
	cmd.Env = env
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &asked{cmd: cmd, out: &out}, nil
}

// wait returns the exit code and what the ask printed.
func (a *asked) wait() (int, string) {
	err := a.cmd.Wait()
	code := 0
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return code, strings.TrimSpace(a.out.String())
}

func runSeal(args []string) error {
	fs := flag.NewFlagSet("seal", flag.ExitOnError)
	device := fs.String("device", "", "push.pair params written by the app")
	hostID := fs.String("host-id", "", "the helper's host id")
	out := fs.String("out", "", "the .apns file to write")
	requestID := fs.String("request-id", "r_e2eapprove", "request id")
	title := fs.String("title", "Run make deploy?", "the question")
	options := fs.String("options", "Approve,Deny", "comma-separated answers")
	fs.Parse(args)
	p, err := readDevice(*device)
	if err != nil {
		return err
	}
	pt := wire.PushPlaintext{V: 1, Kind: wire.PushNeedsYou, Title: *title, RequestID: *requestID, TS: time.Now().UnixMilli()}
	// The first option approves and the second denies, as the helper infers for
	// Approve,Deny.
	roles := []string{wire.RoleApprove, wire.RoleDeny}
	for i, label := range strings.Split(*options, ",") {
		role := wire.RoleOther
		if i < len(roles) {
			role = roles[i]
		}
		pt.Options = append(pt.Options, wire.Option{N: i + 1, Label: label, Role: role})
	}
	payload, err := sealedAPNs(p, *hostID, pt)
	if err != nil {
		return err
	}
	return os.WriteFile(*out, payload, 0o600)
}

// sealedAPNs seals pt for the device as the helper does (label push, aad
// hostId|deviceId) and wraps it in the relay's alert payload.
func sealedAPNs(p wire.PairParams, hostID string, pt wire.PushPlaintext) ([]byte, error) {
	var pub [32]byte
	key, err := base64.RawURLEncoding.DecodeString(p.X25519)
	if err != nil || len(key) != 32 {
		return nil, errors.New("device x25519 key unreadable")
	}
	copy(pub[:], key)
	raw, err := json.Marshal(pt)
	if err != nil {
		return nil, err
	}
	ct, err := seal.Seal(pub, raw, seal.LabelPush, []byte(hostID+"|"+p.DeviceID))
	if err != nil {
		return nil, err
	}
	payload, _ := apnsPayload("alert", ct)
	return payload, nil
}
