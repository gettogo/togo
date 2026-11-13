package daemon

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"github.com/gettogo/togo/helper/internal/mailbox"
	"github.com/gettogo/togo/helper/internal/wire"
	"time"
)

// runMailbox starts the mailbox reader once a device is paired, so a host
// that never pairs never creates keys or dials the relay.
func (c *Components) runMailbox(ctx context.Context) {
	t := time.NewTicker(mailboxPoll)
	defer t.Stop()
	for len(c.mailboxDevices()) == 0 {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
	k, err := c.Push.HostKeys()
	if err != nil {
		c.logf("mailbox: host keys: %v", err)
		return
	}
	err = mailbox.Run(ctx, mailbox.Config{
		Keys:    mailbox.Keys{HostID: k.HostID, Private: k.X25519Priv, MailboxSecret: k.MailboxSecret[:]},
		Devices: c.mailboxDevices,
		Options: c.pendingOptions,
		Answer:  c.Approvals.Answer,
		// A device registers new tokens through the mailbox; the next push
		// reads them from the store.
		Register:     c.Push.Devices().UpdateTokens,
		ContextReady: c.Push.Devices().ContextReady,
		// A dismissed or ended activity no longer stands in for a push.
		EndLive:    c.Push.Devices().EndLiveActivity,
		Logf:       c.logf,
		Will:       c.Will,
		StopMarker: c.stopMarker,
	})
	if err != nil {
		c.logf("mailbox: %v", err)
	}
}

// mailboxDevices converts the pairing store for the reader. Unpaired
// devices are absent and dead ones are marked, so answers signed by either
// are refused.
func (c *Components) mailboxDevices() []mailbox.Device {
	devs, err := c.Push.Devices().List()
	if err != nil {
		c.logf("mailbox: devices: %v", err)
		return nil
	}
	out := make([]mailbox.Device, 0, len(devs))
	for _, d := range devs {
		key, err := base64.RawURLEncoding.DecodeString(d.ApprovalKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			continue
		}
		out = append(out, mailbox.Device{
			DeviceID: d.DeviceID, ApprovalKey: ed25519.PublicKey(key),
			RelayURL: d.RelayURL, Dead: d.Dead, RegisterTS: d.RegisterTS,
		})
	}
	return out
}

func (c *Components) pendingOptions(requestID string) ([]wire.Option, bool) {
	for _, a := range c.Approvals.Pending("") {
		if a.RequestID == requestID {
			return a.Options, true
		}
	}
	return nil, false
}
