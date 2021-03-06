package ghch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"text/template"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/octokit/go-octokit/octokit"
)

type ghOpts struct {
	RepoPath    string `short:"r" long:"repo" default:"." description:"git repository path"`
	GitPath     string `short:"g" long:"git" default:"git" description:"git path"`
	From        string `short:"f" long:"from" description:"git commit revision range start from"`
	To          string `short:"t" long:"to" description:"git commit revision range end to"`
	Token       string `          long:"token" description:"github token"`
	Verbose     bool   `short:"v" long:"verbose"`
	Remote      string `          long:"remote" default:"origin" description:"default remote name"`
	Format      string `short:"F" long:"format" default:"json" description:"json or markdown"`
	All         bool   `short:"A" long:"all" description:"output all changes"`
	NextVersion string `short:"N" long:"next-version"`
	// Tmpl string
}

const (
	exitCodeOK = iota
	exitCodeParseFlagError
	exitCodeErr
)

// CLI is struct for command line tool
type CLI struct {
	OutStream, ErrStream io.Writer
}

// Run the ghch
func (cli *CLI) Run(argv []string) int {
	log.SetOutput(cli.ErrStream)
	p, opts, err := parseArgs(argv)
	if err != nil {
		if ferr, ok := err.(*flags.Error); !ok || ferr.Type != flags.ErrHelp {
			p.WriteHelp(cli.ErrStream)
		}
		return exitCodeParseFlagError
	}

	gh := (&ghch{
		remote:   opts.Remote,
		repoPath: opts.RepoPath,
		gitPath:  opts.GitPath,
		verbose:  opts.Verbose,
		token:    opts.Token,
	}).initialize()

	if opts.All {
		chlog := Changelog{}
		vers := append(gh.versions(), "")
		prevRev := ""
		for _, rev := range vers {
			r := gh.getSection(rev, prevRev)
			if prevRev == "" && opts.NextVersion != "" {
				r.ToRevision = opts.NextVersion
			}
			chlog.Sections = append(chlog.Sections, r)
			prevRev = rev
		}

		if opts.Format == "markdown" {
			results := make([]string, len(chlog.Sections))
			for i, v := range chlog.Sections {
				results[i], _ = v.toMkdn()
			}
			fmt.Fprintln(cli.OutStream, strings.Join(results, "\n\n"))
		} else {
			jsn, _ := json.MarshalIndent(chlog, "", "  ")
			fmt.Fprintln(cli.OutStream, string(jsn))
		}
	} else {
		if opts.From == "" && opts.To == "" {
			opts.From = gh.getLatestSemverTag()
		}
		r := gh.getSection(opts.From, opts.To)
		if r.ToRevision == "" && opts.NextVersion != "" {
			r.ToRevision = opts.NextVersion
		}
		if opts.Format == "markdown" {
			str, err := r.toMkdn()
			if err != nil {
				log.Print(err)
			} else {
				fmt.Fprintln(cli.OutStream, str)
			}
		} else {
			jsn, _ := json.MarshalIndent(r, "", "  ")
			fmt.Fprintln(cli.OutStream, string(jsn))
		}
	}
	return exitCodeOK
}

func parseArgs(args []string) (*flags.Parser, *ghOpts, error) {
	opts := &ghOpts{}
	p := flags.NewParser(opts, flags.Default)
	p.Usage = "[OPTIONS]\n\nVersion: " + version
	_, err := p.ParseArgs(args)
	return p, opts, err
}

func (gh *ghch) getSection(from, to string) Section {
	r := gh.mergedPRs(from, to)
	t, err := gh.getChangedAt(to)
	if err != nil {
		log.Print(err)
	}
	owner, repo := gh.ownerAndRepo()
	return Section{
		PullRequests: r,
		FromRevision: from,
		ToRevision:   to,
		ChangedAt:    t,
		Owner:        owner,
		Repo:         repo,
	}
}

// Changelog contains Sectionst
type Changelog struct {
	Sections []Section `json:"Sections"`
}

// Section contains changes between two revisions
type Section struct {
	PullRequests []*octokit.PullRequest `json:"pull_requests"`
	FromRevision string                 `json:"from_revision"`
	ToRevision   string                 `json:"to_revision"`
	ChangedAt    time.Time              `json:"changed_at"`
	Owner        string                 `json:"owner"`
	Repo         string                 `json:"repo"`
}

var tmplStr = `{{$ret := . -}}
## [{{.ToRevision}}](https://github.com/{{.Owner}}/{{.Repo}}/releases/tag/{{.ToRevision}}) ({{.ChangedAt.Format "2006-01-02"}})
{{range .PullRequests}}
* {{.Title}} [#{{.Number}}](https://github.com/{{$ret.Owner}}/{{$ret.Repo}}/pull/{{.Number}}) ([{{.User.Login}}](https://github.com/{{.User.Login}}))
{{- end}}`

var mdTmpl *template.Template

func init() {
	var err error
	mdTmpl, err = template.New("md-changelog").Parse(tmplStr)
	if err != nil {
		log.Fatal(err)
	}
}

func (rs Section) toMkdn() (string, error) {
	var b bytes.Buffer
	err := mdTmpl.Execute(&b, rs)
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
