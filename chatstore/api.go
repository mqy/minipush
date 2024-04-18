package chatstore

type SenderKind string

const (
	ChatKind_Two   = "two" //  one-on-one, two-party
	ChatKind_Group = "group"
	ChatKind_Topic = "topic"
)

// content spec:
// html(), text(size), link(name,url), image(name,type), file(name),
// audio(size,type,duration), video(size,type,duration), phone(duration)

// Other:
// - read receipts
// - redraw
// - force deleted

type Msg struct {
	Sequence  int64
	Sender    string // user(id), bot(id)
	Recipient string // user(id), group(id), topic(id)
	Content   string
}
