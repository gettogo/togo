package gitx

import (
	"bytes"
	"context"
)

// Branch is one local branch.
type Branch struct {
	Name     string  `json:"name"`
	Upstream *string `json:"upstream"`
	Current  bool    `json:"current"`
}

// Branches is the git.branches result.
type Branches struct {
	Current  *string  `json:"current"`
	Branches []Branch `json:"branches"`
}

// Branches lists local branches by name. Outside a repository it is empty.
func (g *Git) Branches(ctx context.Context, dir string) (Branches, error) {
	res := Branches{Branches: []Branch{}}
	root, err := g.Toplevel(ctx, dir)
	if err != nil || root == "" {
		return res, err
	}
	out, _, err := g.Run(ctx, dir, listLimit, "for-each-ref", "--sort=refname",
		"--format=%(refname:short)%00%(upstream:short)%00%(HEAD)", "refs/heads")
	if err != nil {
		return res, err
	}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		f := bytes.Split(line, []byte{0})
		if len(f) != 3 {
			continue
		}
		b := Branch{Name: string(f[0]), Current: string(f[2]) == "*"}
		if len(f[1]) > 0 {
			up := string(f[1])
			b.Upstream = &up
		}
		if b.Current {
			name := b.Name
			res.Current = &name
		}
		res.Branches = append(res.Branches, b)
	}
	return res, nil
}
