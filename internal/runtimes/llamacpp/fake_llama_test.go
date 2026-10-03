package llamacpp

// fakeLlamaEnv, when set, makes the test binary act as llama-server: the
// value is a file to write the pid of a child it starts, so a test can check
// that stopping the model ends the child too.
const fakeLlamaEnv = "YGG_FAKE_LLAMA"
