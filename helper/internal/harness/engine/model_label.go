package engine

import (
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// modelLabels is what the model picker calls each model id: harnesses
// record the id ("anthropic/claude-sonnet-5") where the picker shows its
// name ("Claude Sonnet 5"), and the chip and the chat's model notices say
// the name the user picked. The names are learnt each time the picker is
// built, from the list it shows; until then the id stands.
type modelLabels struct {
	byID map[string]string
}

// learnModelLabelsLocked keeps the names a built model picker shows, and
// shows them on the status block at once.
func (s *sess) learnModelLabelsLocked(sc spec.Screen, p *harness.Picker) {
	if sc.Capability != harness.CapModel || p == nil {
		return
	}
	byID := make(map[string]string, len(p.Items))
	for _, it := range p.Items {
		if it.Title != "" && it.Title != it.ID {
			byID[it.ID] = it.Title
		}
	}
	if len(byID) == 0 {
		return
	}
	s.labels.byID = byID
	s.refreshStatusLocked()
}

// modelLabelLocked is the picker's name for a model id, else the id.
func (s *sess) modelLabelLocked(id string) string {
	if l := s.labels.byID[id]; l != "" {
		return l
	}
	return id
}

// labelModelNoticeLocked says a model notice's model by its name: the
// longest known id the text ends with is replaced ("Model
// anthropic/claude-sonnet-5" becomes "Model Claude Sonnet 5").
func (s *sess) labelModelNoticeLocked(it *harness.Item) {
	if it.Kind != harness.KindNotice || it.Notice == nil || it.Notice.Code != harness.NoticeModel {
		return
	}
	best := ""
	for id := range s.labels.byID {
		if len(id) > len(best) && strings.HasSuffix(it.Notice.Text, id) {
			best = id
		}
	}
	if best == "" {
		return
	}
	n := *it.Notice
	n.Text = strings.TrimSuffix(n.Text, best) + s.labels.byID[best]
	it.Notice = &n
}
