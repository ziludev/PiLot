// What to call a task in a list. A task's kind is for the scheduler; a row in
// a window needs the thing someone asked for, so each kind says where in its
// input the human sentence lives.

const maxTitle = 60;

/** Collapse whitespace and cut to something that fits one line of a list. */
function shorten(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim();
  const runes = [...flat];
  return runes.length <= maxTitle ? flat : `${runes.slice(0, maxTitle).join("")}…`;
}

/**
 * A routine's text opens with who it is and how it should behave; the task is
 * the line that says what to do. Prefer that line, else the first one.
 */
function routineTitle(text: string): string {
  const lines = text.split("\n").map((line) => line.trim()).filter((line) => line !== "");
  for (const line of lines) {
    const goal = /^(?:目标|goal)\s*[:：]\s*(.+)$/i.exec(line);
    if (goal !== null) return shorten(goal[1]!);
  }
  return shorten(lines[0] ?? "");
}

/** The title of a task, or undefined when its kind has nothing to say. */
export function taskTitle(kind: string, input: unknown): string | undefined {
  const value = input as { text?: unknown; spec?: unknown; prompt?: unknown } | undefined;
  const text = typeof value?.text === "string" ? value.text : undefined;
  switch (kind) {
    case "app.reminder":
      return text === undefined ? undefined : shorten(text);
    case "app.routine":
      return text === undefined ? undefined : routineTitle(text);
    case "app.feature":
      return typeof value?.spec === "string" ? shorten(value.spec) : undefined;
    default:
      return undefined;
  }
}
