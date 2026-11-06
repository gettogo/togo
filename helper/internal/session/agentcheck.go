package session

import (
	"context"

	"github.com/gettogo/togo/helper/internal/state"
)

// CheckProgram looks program up the way a custom session would run it: a
// bare name on the login PATH, or a clean absolute path. It returns the
// absolute path found, or installed false when nothing is there. A program
// that could never be launched is an *InvalidParamsError with Reason
// wire.ReasonProgram.
func (s *Service) CheckProgram(ctx context.Context, program string) (path string, installed bool, err error) {
	path, err = findProgram(program, s.loginPath(ctx), false)
	if err != nil {
		return "", false, err
	}
	return path, path != "", nil
}

func (s *Service) loginPath(ctx context.Context) string {
	if s.Path != "" {
		return s.Path
	}
	return state.LoginPath(ctx)
}
