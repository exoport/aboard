// commands.go — the CLI surface, declared as data.
//
// This replaces flag.VisitAll. The spike built the manifest's flag list by
// walking the global flag registry at runtime, which worked exactly because
// there was one flat flag set and one binary. Under cobra neither holds: flags
// are per-command, the global registry is empty, and a `aboard capabilities` run
// would have reported whatever happened to be registered on the path it took to
// get there — a manifest whose contents depend on which subcommand printed it,
// and therefore a capsHash that moves for no reason a reader could see.
//
// So the surface is declared here and the cobra tree is asserted equal to it
// (see pkg/aboard/cli's parity test). Two things that can disagree, with a test
// that fails when they do, beats one thing that is silently derived from the
// wrong source. It is the same seam as views/*.spec.json: the declaration is
// canonical and the code is checked against it, rather than the code being
// scraped and the scrape believed.

package aboard

// Exit codes. A small table, shared across commands so a code means one thing.
//
//	0  it worked
//	1  it ran and failed — no board, a refused write, a broken connection
//	2  usage: a flag or argument the command cannot act on, detected before
//	   anything was contacted
//	3  `wait` gave up. Distinct from 1 on purpose: "nobody came" and "something
//	   broke" call for different behaviour in the script that asked.
const (
	ExitOK      = 0
	ExitFailed  = 1
	ExitUsage   = 2
	ExitTimeout = 3
)

// defaultOutputFormat is the value every --output-format flag falls back to.
// Declared once here because the declared table is what the parity test asserts
// the cobra tree against: a drifting default would be a real contract break.
const defaultOutputFormat = "human"

// Flag is one declared flag: what a caller may pass, and what it defaults to.
// Type is pflag's own type name ("string", "int", "bool", "duration"), because
// that is what the parity test can read back off the cobra tree.
type Flag struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Def  string `json:"default,omitempty"`
	Doc  string `json:"doc"`
}

// Exit is one exit code a command can produce, and what it means when it does.
type Exit struct {
	Code    int    `json:"code"`
	Meaning string `json:"meaning"`
}

// Command is one subcommand of the board CLI.
type Command struct {
	Name  string `json:"name"`
	Args  string `json:"args,omitempty"`
	Doc   string `json:"doc"`
	Flags []Flag `json:"flags,omitempty"`
	Exits []Exit `json:"exits,omitempty"`
}

// RootFlags are the flags on the root command itself, inherited by every
// subcommand.
func RootFlags() []Flag {
	return []Flag{
		{Name: "cwd", Type: "string", Doc: "directory to resolve the project root from (default: the working directory)"},
		{Name: "name", Type: "string", Doc: "board name, for a second isolated board in the same project (env ABOARD_NAME)"},
	}
}

// commonExits is what almost every command can return. Declared once so a
// command that adds a code adds only the code that is unusual about it.
func commonExits() []Exit {
	return []Exit{
		{Code: ExitOK, Meaning: "done"},
		{Code: ExitFailed, Meaning: "no board running, or the request failed"},
		{Code: ExitUsage, Meaning: "a flag or argument the command cannot act on"},
	}
}

// Commands is the declared command table. Order is the order `aboard --help`
// lists them, which is why it is a slice: it is read by a human top to bottom.
func Commands() []Command {
	return []Command{
		{
			Name: "serve",
			Doc:  "run the board server for this project",
			Flags: []Flag{
				{Name: "base-path", Type: "string", Doc: "serve under a URL prefix, e.g. /aboard (default: the server root)"},
				{Name: "dev", Type: "bool", Def: "false", Doc: "serve the web tree from disk instead of the embedded copy"},
				{Name: "dev-dir", Type: "string", Doc: "with --dev, the web tree to serve (default: pkg/aboard/web under the root)"},
				{Name: "port", Type: "int", Def: "0", Doc: "port to listen on (0 derives one from the project root; env PORT)"},
				{Name: "state", Type: "string", Doc: "state file to serve (default: .aboard/aboard.json under the root)"},
			},
			Exits: commonExits(),
		},
		{
			Name: "status",
			Doc:  "report this project's running board, if any, and the caps beacon",
			Flags: []Flag{
				{Name: "output-format", Type: "string", Def: defaultOutputFormat, Doc: "human, json or yaml"},
			},
			Exits: commonExits(),
		},
		{
			Name: "init",
			Doc:  "create .aboard/ in this directory and write an empty board",
			Flags: []Flag{
				{Name: "example", Type: "bool", Def: "false", Doc: "seed the board with the example tabs compiled into this binary"},
				{Name: "gitignore", Type: "bool", Def: "false", Doc: "append " + GitignoreLine + " to the project's .gitignore if it is not already ignored"},
				{Name: "output-format", Type: "string", Def: defaultOutputFormat, Doc: "human, json or yaml"},
			},
			Exits: commonExits(),
		},
		{
			Name: "apply",
			Doc:  "read a board document on stdin and write it through the running board (compare-and-set)",
			Flags: []Flag{
				{Name: "by", Type: "string", Def: "agent-1", Doc: "actor recorded in lastEditedBy and on every tab this write touched"},
			},
			Exits: commonExits(),
		},
		{
			Name: "wait",
			Doc:  "block until the board is poked or until a predicate matches",
			Flags: []Flag{
				{Name: "by", Type: "string", Def: "agent-1", Doc: "who is waiting; shown on the human's notify button"},
				{Name: "for", Type: "string", Def: "poke", Doc: `what to wait for: poke | change | "tab <id>" | "answer <id>" | "node <id>=<status>"`},
				{Name: "note", Type: "string", Doc: "why you are waiting; shown on the button beside your name"},
				{Name: "timeout", Type: "duration", Def: "10m0s", Doc: "how long to block before giving up"},
			},
			Exits: append(commonExits(), Exit{Code: ExitTimeout, Meaning: "the timeout ran out; nobody poked"}),
		},
		{
			Name: "poke",
			Doc:  "release every session waiting on this board, as the human's notify button does",
			Flags: []Flag{
				{Name: "by", Type: "string", Def: "agent-1", Doc: "who is releasing them"},
				{Name: "note", Type: "string", Doc: "a message for the waiting sessions"},
			},
			Exits: commonExits(),
		},
		{
			Name: "journal",
			Doc:  "print recent accepted writes: when, who, which tabs",
			Flags: []Flag{
				{Name: "limit", Type: "int", Def: "40", Doc: "how many entries to print"},
				{Name: "output-format", Type: "string", Def: defaultOutputFormat, Doc: "human, json or yaml"},
			},
			Exits: commonExits(),
		},
		{
			Name:  "watch",
			Doc:   "follow every change as JSON lines until interrupted",
			Exits: commonExits(),
		},
		{
			Name:  "log",
			Args:  "<tab>",
			Doc:   "read stdin and append it to a tab's sidecar log, line by line",
			Exits: commonExits(),
		},
		{
			Name: "export",
			Args: "<tab|key>",
			Doc:  "print one tab as text, for pasting into the project's own documents",
			Flags: []Flag{
				{Name: "format", Type: "string", Def: "md", Doc: "md or csv"},
			},
			Exits: commonExits(),
		},
		{
			Name: "capabilities",
			Args: "[type]",
			Doc:  "print what this board can do: types, state fields, controls, endpoints, commands",
			Flags: []Flag{
				{Name: "check", Type: "bool", Def: "false", Doc: "exit 1 if the committed skill reference is stale"},
				{Name: "format", Type: "string", Def: "json", Doc: "json, md, or js (the generated control module)"},
			},
			Exits: []Exit{
				{Code: ExitOK, Meaning: "printed; or with --check, the committed reference is current"},
				{Code: ExitFailed, Meaning: "with --check: the committed reference no longer matches the binary"},
				{Code: ExitUsage, Meaning: "no such tab type, or an unknown --format"},
			},
		},
		{
			Name: "recipes",
			Doc:  "list the recipes available here, or print one",
			Exits: []Exit{
				{Code: ExitOK, Meaning: "printed"},
				{Code: ExitFailed, Meaning: "no such recipe, or the recipe has no template"},
				{Code: ExitUsage, Meaning: "an unknown output format"},
			},
		},
		{
			Name: "version",
			Doc:  "print the build identity of this binary",
			Flags: []Flag{
				{Name: "output-format", Type: "string", Def: defaultOutputFormat, Doc: "human, json or yaml"},
			},
			Exits: commonExits(),
		},
	}
}
