package process

// Command preserves the executable and argv independently, including login argv[0].
type Command struct {
	Path string
	Args []string
}
