package ctl

// NewPendingForTest builds a Pending outside the socket server, with the
// channel its reply lands on. For tests of request handlers.
func NewPendingForTest(req Request) (*Pending, <-chan Response) {
	ch := make(chan Response, 1)
	return &Pending{Req: req, reply: ch}, ch
}
