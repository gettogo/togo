package surface

import (
	"errors"
	"time"

	"github.com/gettogo/togo/helper/internal/rpc"
)

const ReadChunkBytes = 64 << 10

// Read retains one immutable snapshot across all of its chunks. Reading is
// also the visible renderer heartbeat; background clients must stop polling.
func (s *Service) Read(owner *rpc.Conn, p ReadParams) (ReadResult, error) {
	r, err := s.lookup(owner, p.ID, p.Lease)
	if err != nil {
		return ReadResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ReadResult{}, errLease
	}
	result := ReadResult{Generation: r.lease.Generation}
	if p.Offset < 0 {
		return result, ErrMalformed
	}
	if p.Snapshot == "" {
		if p.Offset != 0 {
			return result, ErrMalformed
		}
		raw, revision, unchanged, err := r.cache.snapshot(p.Revision)
		if errors.Is(err, ErrCachePending) {
			result.Pending = true
			return result, nil
		}
		if err != nil {
			return result, err
		}
		r.lastRead = time.Now()
		result.Revision = revision
		if unchanged {
			result.Unchanged = true
			return result, nil
		}
		if len(raw) > maxSnapshotBytes {
			return result, ErrLimit
		}
		r.snapshot, r.snapshotID, r.snapshotRevision = raw, randomID(), revision
	} else if p.Snapshot != r.snapshotID || r.snapshotID == "" {
		return result, errLease
	}
	if p.Offset > len(r.snapshot) {
		return result, ErrMalformed
	}
	r.lastRead = time.Now()
	end := min(len(r.snapshot), p.Offset+ReadChunkBytes)
	result.Snapshot, result.Revision = r.snapshotID, r.snapshotRevision
	result.Offset, result.NextOffset, result.Total = p.Offset, end, len(r.snapshot)
	result.Data = append([]byte(nil), r.snapshot[p.Offset:end]...)
	return result, nil
}
