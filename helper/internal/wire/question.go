package wire

// MethodLocalQuestion is sent by a blocking Claude Code AskUserQuestion
// hook: it holds the question dialog until the phone answers.
const MethodLocalQuestion = "local.question"

// Question limits.
const (
	MaxQuestions      = 16
	MaxQuestionPrompt = 2000
)

// QuestionParams are local.question params: the prompts of the questions
// the dialog asks, in order, as the tool input carries them.
type QuestionParams struct {
	SessionID  *string  `json:"sessionId"`
	Questions  []string `json:"questions"`
	TimeoutSec int      `json:"timeoutSec"`
}

// QuestionResult answers local.question. Answers maps each prompt to the
// chosen label, labels joined with ", " for a multi-select, or the typed
// text; it is nil on timeout.
type QuestionResult struct {
	Answers map[string]string `json:"answers,omitempty"`
	Reason  string            `json:"reason"`
}
