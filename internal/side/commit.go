package side

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// CommitProps is the commit message dialog.
type CommitProps struct {
	Message   string // bound value
	Open      bool
	Busy      bool
	OnMessage func(string) // called with the edited message
	OnCancel  func()
	OnSubmit  func()
}

// CommitDialog draws the modal that types and submits a commit message. Enter
// submits, Shift+Enter stays for a second line of thought, and a click on the
// backdrop cancels.
func CommitDialog(c *ui.Context, p CommitProps) {
	st := stateFor(c)
	if !p.Open {
		st.commitOpen = false
		return
	}
	if !st.commitOpen {
		st.commitOpen = true
		st.commitMessage = p.Message
	}
	t := theme.Current(c)
	open := true
	submitted := false
	cancelled := false
	ui.Modal(c, &open, func() {
		ui.Text(c, i18n.T("commit")).FontSize(13).FontWeight(600).TextColor(t.Foreground)
		ui.TextInput(c, &st.commitMessage).Width(320).FontSize(12).AutoFocus().
			Placeholder(i18n.T("commitMessage")).OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods&ui.Shift != 0 {
				return false
			}
			if !p.Busy {
				submitted = true
				if p.OnSubmit != nil {
					p.OnSubmit()
				}
			}
			return true
		})
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			ui.Button(c, i18n.T("cancel")).OnClick(func() {
				cancelled = true
				if p.OnCancel != nil {
					p.OnCancel()
				}
			})
			ui.PrimaryButton(c, i18n.T("commit")).Disabled(p.Busy).OnClick(func() {
				if p.Busy {
					return
				}
				submitted = true
				if p.OnSubmit != nil {
					p.OnSubmit()
				}
			})
		})
	})
	if p.OnMessage != nil && st.commitMessage != p.Message {
		p.OnMessage(st.commitMessage)
	}
	if !open && !submitted && !cancelled {
		st.commitOpen = false
		if p.OnCancel != nil {
			p.OnCancel()
		}
	}
}
