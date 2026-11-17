package turns

import "pitta/internal/protocol"

// The two calls that hand work to a child conversation. Both leave their
// children behind in the result's details, which is the only place the parent
// conversation can learn who worked on it.
const (
	DelegateTool         = "delegate"
	DelegateParallelTool = "delegate_parallel"
)

// Child is one conversation a delegate call ran: the role it ran under, where it
// lives, and whether it came back broken. ConversationID is zero when the tool
// did not say — an older parallel batch only reported roles and a failure count.
type Child struct {
	Role           string
	ConversationID int
	Failed         bool
}

// Delegation is a delegate call as the tool recorded it.
type Delegation struct {
	Children []Child
	// Failed counts the children the tool says came back broken without saying
	// which ones. Newer results name them per child; this covers the older shape.
	Failed int
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
	// Per-child detail, as delegate_parallel now writes it.
	if children, ok := details["children"].([]any); ok {
		for _, raw := range children {
			child, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			role, _ := child["role"].(string)
			if role == "" {
				continue
			}
			failed, _ := child["failed"].(bool)
			out.Children = append(out.Children, Child{
				Role:           role,
				ConversationID: intOf(child["conversationId"]),
				Failed:         failed,
			})
		}
	}
	// One child, as delegate writes it.
	if role, ok := details["role"].(string); ok && role != "" {
		failed, _ := details["failed"].(bool)
		out.Children = append(out.Children, Child{
			Role:           role,
			ConversationID: intOf(details["conversationId"]),
			Failed:         failed,
		})
	}
	// Roles without a home, as an older parallel batch wrote them.
	if len(out.Children) == 0 {
		if roles, ok := details["roles"].([]any); ok {
			for _, role := range roles {
				if name, ok := role.(string); ok && name != "" {
					out.Children = append(out.Children, Child{Role: name})
				}
			}
		}
	}
	out.Failed = intOf(details["failed"])
	if len(out.Children) == 0 {
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
