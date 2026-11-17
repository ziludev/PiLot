package turns

import "pitta/internal/protocol"

// The two calls that hand work to a child conversation. Both leave their
// children behind in the result's details, which is the only place the parent
// conversation can learn who worked on it.
const (
	DelegateTool         = "delegate"
	DelegateParallelTool = "delegate_parallel"
)

// Delegation is a delegate call as the tool recorded it: the roles that ran, the
// child conversation a single one lives in, and how many came back broken.
//
// Parallel children carry no conversation id — the tool only reports the roles
// and how many failed — so ConversationID is zero for them and a view has
// nothing to open.
type Delegation struct {
	Roles          []string
	ConversationID int
	Failed         int
}

// DelegationOf reads what a delegate call left in its result's details. ok is
// false for every other call, and for a delegate whose child has not answered
// yet — a call still in flight has nothing to report.
func DelegationOf(entry protocol.Entry) (Delegation, bool) {
	list := messages(entry.Model)
	if len(list) == 0 {
		return Delegation{}, false
	}
	tool, _ := list[0]["toolName"].(string)
	if tool != DelegateTool && tool != DelegateParallelTool {
		return Delegation{}, false
	}
	details, ok := list[0]["details"].(map[string]any)
	if !ok {
		return Delegation{}, false
	}
	var out Delegation
	if role, ok := details["role"].(string); ok && role != "" {
		out.Roles = append(out.Roles, role)
		out.ConversationID = intOf(details["conversationId"])
	}
	if roles, ok := details["roles"].([]any); ok {
		for _, role := range roles {
			if name, ok := role.(string); ok && name != "" {
				out.Roles = append(out.Roles, name)
			}
		}
	}
	out.Failed = intOf(details["failed"])
	if len(out.Roles) == 0 {
		return Delegation{}, false
	}
	return out, true
}

// intOf reads a count or id the bridge encoded as a JSON number.
func intOf(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}
