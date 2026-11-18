package harness

// MaxQuestions is the most questions of one item an app decodes and shows.
// A mapper that has more shows the first MaxQuestions with Answerable
// false, so nothing is cut and then offered for an incomplete answer; the
// answer action takes at most this many answers.
const MaxQuestions = 64
