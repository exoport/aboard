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

// The two strings the table below and the cobra tree in package cli must spell
// IDENTICALLY — TestFlagParity compares `--output-format`'s help text and
// `--by`'s default byte for byte across the two. Exported for that reason and no
// other: the parity test can only report a disagreement after it has happened,
// whereas one constant makes the agreement structural. Anything else the two
// files share is caught by that test and stays a literal on both sides, next to
// the variable it binds.
const (
	// UsageOutputFormat is the help line of every --output-format flag.
	UsageOutputFormat = "human, json or yaml"
	// DefaultActor is what --by falls back to when an agent does not say who it is.
	DefaultActor = "agent-1"
)

// pflag's own type names, as Flag.Type must report them: the parity test reads
// them back off the cobra tree with Value.Type(), so these are pflag's spelling,
// not ours.
const (
	flagTypeString   = "string"
	flagTypeBool     = "bool"
	flagTypeInt      = "int"
	flagTypeDuration = "duration"
)

// meaningPrinted is what ExitOK means for a command whose whole job is to print.
const meaningPrinted = "printed"

// Flag names and one default that the table below repeats. Unexported, unlike
// the two above: a name that drifts is caught STRUCTURALLY by TestFlagParity —
// it walks the cobra tree flag by flag and reports the one side that has a flag
// the other does not — whereas a drifting help string or default is only caught
// as a diff, which is why those two are shared with package cli and these are
// not. The cobra side keeps its literal next to the variable it binds.
const (
	flagNameBy           = "by"
	flagNameOutputFormat = "output-format"

	// defFalse is how pflag renders a bool flag's default, which is what the
	// parity test reads back off the tree.
	defFalse = "false"
)

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
//
// Subcommands is the same shape one level down. It exists because `recipes` is
// a group whose real surface — `recipes list --output-format`, `recipes show
// --template` — sat one level below anything the manifest or the parity test
// looked at, so a flag could be added to either with the suite green and
// capsHash unmoved. A tree that stops at depth one is a declaration that lies
// about a tree.
type Command struct {
	Name        string    `json:"name"`
	Args        string    `json:"args,omitempty"`
	Doc         string    `json:"doc"`
	Flags       []Flag    `json:"flags,omitempty"`
	Exits       []Exit    `json:"exits,omitempty"`
	Subcommands []Command `json:"subcommands,omitempty"`
}

// RootFlags are the flags on the root command itself, inherited by every
// subcommand.
func RootFlags() []Flag {
	return []Flag{
		{Name: "cwd", Type: flagTypeString, Doc: "directory to resolve the project root from (default: the working directory)"},
		{Name: "name", Type: flagTypeString, Doc: "board name, for a second isolated board in the same project (env ABOARD_NAME)"},
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

// Commands is the declared command table. It is a slice because its ORDER is part
// of the surface: it is the order the capability manifest reports and the order
// the generated skill reference prints, which is what a reader goes through top
// to bottom — most-used first, rather than alphabetically.
//
// It is NOT the order `aboard --help` prints, and the comment here used to say it
// was. Cobra sorts its command list alphabetically, and the switch for that
// (`cobra.EnableCommandSorting`) is a PACKAGE-LEVEL variable — so setting it
// would reorder the commands of any host that mounts this tree, which is the
// package-level cobra state this library exists without (see aboard.go). A
// cosmetic ordering is not worth reaching into a host's own help output, so the
// claim was corrected rather than made true.
//
// Reordering this slice therefore moves capsHash, which is correct: the manifest
// reports it.
func Commands() []Command {
	return []Command{
		{
			Name: "serve",
			Doc:  "run the board server for this project",
			Flags: []Flag{
				{Name: "base-path", Type: flagTypeString, Doc: "serve under a URL prefix, e.g. /aboard (default: the server root)"},
				{Name: "dev", Type: flagTypeBool, Def: defFalse, Doc: "serve the web tree from disk instead of the embedded copy"},
				{Name: "dev-dir", Type: flagTypeString, Doc: "with --dev, the web tree to serve (default: pkg/aboard/web under the root)"},
				{Name: "port", Type: flagTypeInt, Def: "0", Doc: "port to listen on (0 derives one from the project root; env PORT)"},
				{Name: "state", Type: flagTypeString, Doc: "state file to serve (default: .aboard/aboard.json under the root)"},
			},
			Exits: commonExits(),
		},
		{
			Name: "status",
			Doc:  "report this project's running board, if any, and the caps beacon",
			Flags: []Flag{
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
			},
			Exits: commonExits(),
		},
		{
			Name: "init",
			Doc:  "create .aboard/ in this directory and write an empty board",
			Flags: []Flag{
				{Name: "example", Type: flagTypeBool, Def: defFalse, Doc: "seed the board with the example tabs compiled into this binary"},
				{Name: "gitignore", Type: flagTypeBool, Def: defFalse, Doc: "append " + GitignoreLine + " to the project's .gitignore if it is not already ignored"},
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
			},
			Exits: commonExits(),
		},
		{
			Name: "apply",
			Doc:  "read a board document on stdin and write it through the running board (compare-and-set)",
			Flags: []Flag{
				{Name: flagNameBy, Type: flagTypeString, Def: DefaultActor, Doc: "actor recorded in lastEditedBy and on every tab this write touched"},
				{Name: "check", Type: flagTypeBool, Def: defFalse, Doc: "run the write warnings and stop: nothing is posted, and no board need be running"},
				{Name: "force", Type: flagTypeBool, Def: defFalse, Doc: "write without compare-and-set, overwriting anything since you read the document"},
				{Name: "label", Type: flagTypeString, Doc: "why this write is happening; recorded on the journal entry, not in the board"},
				{Name: "strict", Type: flagTypeBool, Def: defFalse, Doc: "refuse the write if anything warns (exit 1, nothing written)"},
			},
			Exits: commonExits(),
		},
		{
			Name: "wait",
			Doc:  "block until the board is poked or until a predicate matches",
			Flags: []Flag{
				{Name: flagNameBy, Type: flagTypeString, Def: DefaultActor, Doc: "who is waiting; shown on the human's notify button"},
				{Name: "for", Type: flagTypeString, Def: "poke", Doc: `what to wait for: poke | change | "tab <id>" | "answer <id>" | "node <id>=<status>" | "rendered <id>"`},
				{Name: "note", Type: flagTypeString, Doc: "why you are waiting; shown on the button beside your name"},
				{Name: "timeout", Type: flagTypeDuration, Def: "10m0s", Doc: "how long to block before giving up"},
			},
			Exits: append(commonExits(), Exit{Code: ExitTimeout, Meaning: "the timeout ran out; nobody poked"}),
		},
		{
			Name: "poke",
			Doc:  "release every session waiting on this board, as the human's notify button does",
			Flags: []Flag{
				{Name: flagNameBy, Type: flagTypeString, Def: DefaultActor, Doc: "who is releasing them"},
				{Name: "note", Type: flagTypeString, Doc: "a message for the waiting sessions"},
			},
			Exits: commonExits(),
		},
		{
			Name: "journal",
			Doc:  "print recent accepted writes: when, who, which tabs",
			Flags: []Flag{
				{Name: "limit", Type: flagTypeInt, Def: "40", Doc: "how many entries to print"},
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
			},
			Exits: commonExits(),
		},
		{
			Name: "history",
			Args: "<tab>",
			Doc:  "list what a tab said before, from the journal; --at N prints a document `apply` accepts",
			Flags: []Flag{
				{Name: "at", Type: flagTypeInt, Def: "0", Doc: "print the document that restores version N instead of listing (1 is the most recent)"},
				{Name: "limit", Type: flagTypeInt, Def: "20", Doc: "how many versions to list"},
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
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
			Name: "rendered",
			Args: "[tab]",
			Doc:  "print what the browser reported it drew: control ids, presses, and unknown-component markers",
			Flags: []Flag{
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
			},
			Exits: commonExits(),
		},
		{
			Name: "uploads",
			Doc:  "list the files under .aboard/uploads/ with their size and the tabs that mention them",
			Flags: []Flag{
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
				{Name: "prune", Type: flagTypeBool, Def: defFalse, Doc: "show which unreferenced files would be deleted"},
				{Name: "yes", Type: flagTypeBool, Def: defFalse, Doc: "with --prune, actually delete them"},
			},
			Exits: commonExits(),
		},
		{
			Name: "export",
			Args: "<tab|key>",
			Doc:  "print one tab as text, for pasting into the project's own documents",
			Flags: []Flag{
				{Name: "format", Type: flagTypeString, Def: "md", Doc: "md or csv"},
			},
			Exits: commonExits(),
		},
		{
			Name: "capabilities",
			Args: "[type]",
			Doc:  "print what this board can do: types, state fields, controls, endpoints, commands",
			Flags: []Flag{
				{Name: "check", Type: flagTypeBool, Def: defFalse, Doc: "exit 1 if the committed skill reference is stale"},
				{Name: "format", Type: flagTypeString, Def: "json", Doc: "json, md, or js (the generated control module)"},
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
				{Code: ExitOK, Meaning: meaningPrinted},
				{Code: ExitFailed, Meaning: "no such recipe, or the recipe has no template"},
				{Code: ExitUsage, Meaning: "an unknown output format"},
			},
			Subcommands: []Command{
				{
					Name: "list",
					Doc:  "list every recipe available in this project, and where each came from",
					Flags: []Flag{
						{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
					},
					Exits: []Exit{
						{Code: ExitOK, Meaning: meaningPrinted},
						{Code: ExitFailed, Meaning: "the recipe directories could not be read"},
						{Code: ExitUsage, Meaning: "an unknown output format"},
					},
				},
				{
					Name: "show",
					Args: "<name>",
					Doc:  "print one recipe's body, or just its tab skeleton",
					Flags: []Flag{
						{Name: "template", Type: flagTypeBool, Def: defFalse, Doc: "print only the recipe's JSON tab skeleton"},
					},
					Exits: []Exit{
						{Code: ExitOK, Meaning: meaningPrinted},
						{Code: ExitFailed, Meaning: "no such recipe, it does not parse, or it has no template"},
						{Code: ExitUsage, Meaning: "a missing or extra argument"},
					},
				},
			},
		},
		{
			Name: "version",
			Doc:  "print the build identity of this binary",
			Flags: []Flag{
				{Name: flagNameOutputFormat, Type: flagTypeString, Def: defaultOutputFormat, Doc: UsageOutputFormat},
			},
			Exits: commonExits(),
		},
	}
}
