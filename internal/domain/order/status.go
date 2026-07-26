package order

// Status is the lifecycle state of an Order. The aggregate enforces the allowed
// transitions: draft -> placed -> paid, with cancel available until paid.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusPlaced    Status = "placed"
	StatusPaid      Status = "paid"
	StatusCancelled Status = "cancelled"
)

// isFinal reports whether the status is terminal (no further transitions).
func (s Status) isFinal() bool {
	return s == StatusPaid || s == StatusCancelled
}
