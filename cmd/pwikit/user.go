package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/WikitTeam/ProjectWikit/internal/db"
	"github.com/WikitTeam/ProjectWikit/internal/wikidot"
)

func userUsage() {
	fmt.Fprint(os.Stderr, `Usage: pwikit user merge -from <account> -into <account> [options]
       pwikit user prune [-apply] [options]

  merge  move everything one account holds onto another, then delete the first
  prune  list imported Wikidot accounts that no site uses, and with -apply delete them

Name an account by its user name, by wd:<name> for the name it had on Wikidot,
or by #<number>. Quote #<number> in a shell, or it is read as a comment:

  pwikit user merge -from '#3404' -into '#121'

An imported account is in use while it wrote a page, revision, file or post,
voted, holds a role, appears in a log, ticket, report or message, or is named
by a [[user]] block in any page or post.

Options:
  -from      account to merge away
  -into      account that keeps everything
  -apply     delete the accounts prune lists
  -yes       go ahead without asking
  -database  PostgreSQL connection string
  -data-dir  state directory; defaults to the directory holding the executable
`)
}

func userCommand(args []string) error {
	if len(args) == 0 || (args[0] != "merge" && args[0] != "prune") {
		userUsage()
		return errors.New("unknown user subcommand")
	}
	fs := flag.NewFlagSet("user "+args[0], flag.ContinueOnError)
	fs.Usage = userUsage
	from := fs.String("from", "", "account to merge away")
	into := fs.String("into", "", "account that keeps everything")
	apply := fs.Bool("apply", false, "delete the accounts prune lists")
	yes := fs.Bool("yes", false, "go ahead without asking")
	database := fs.String("database", os.Getenv(envDatabase), "PostgreSQL connection string")
	dataDir := fs.String("data-dir", "", "state directory; defaults to the directory holding the executable")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if args[0] == "prune" {
		return pruneUsers(*database, *dataDir, *apply, *yes)
	}
	if *from == "" || *into == "" {
		userUsage()
		return errors.New("give both -from and -into")
	}

	ctx := context.Background()
	dsn, release, err := resolveDatabase(ctx, *database, *dataDir)
	if err != nil {
		return err
	}
	defer release()
	conn, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close()

	source, err := findAccount(ctx, conn, *from)
	if err != nil {
		return err
	}
	target, err := findAccount(ctx, conn, *into)
	if err != nil {
		return err
	}
	if target.Type == db.UserTypeWikidot {
		return fmt.Errorf("%s has not been claimed and cannot sign in; merge it into the other account instead", describeAccount(target))
	}
	if !*yes {
		if err := confirmMerge(bufio.NewReader(os.Stdin), source, target); err != nil {
			return err
		}
	}

	moves, err := conn.MergeUsers(ctx, source.ID, target.ID)
	switch {
	case errors.Is(err, db.ErrMergeSame):
		return fmt.Errorf("-from and -into both name %s", describeAccount(source))
	case errors.Is(err, db.ErrMergeTwoWikidot):
		return fmt.Errorf("%s and %s were imported from two different Wikidot accounts; merging would lose one of them",
			describeAccount(source), describeAccount(target))
	case err != nil:
		return err
	}
	for _, m := range moves {
		fmt.Printf("  %s.%s: %d moved, %d dropped as duplicates\n", m.Table, m.Column, m.Moved, m.Dropped)
	}
	fmt.Printf("merged %s into %s\n", describeAccount(source), describeAccount(target))
	return nil
}

func findAccount(ctx context.Context, conn *db.DB, ref string) (*db.User, error) {
	var (
		found *db.User
		err   error
	)
	switch {
	case strings.HasPrefix(ref, "#"):
		id, perr := strconv.ParseInt(ref[1:], 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("%q is not an account number", ref)
		}
		found, err = conn.UserByID(ctx, id)
	case strings.HasPrefix(strings.ToLower(ref), "wd:"):
		found, err = conn.UserByWikidotAlias(ctx, ref[3:])
	default:
		found, err = conn.UserByUsername(ctx, wikidot.CanonicalizeUsername(ref))
	}
	if errors.Is(err, db.ErrNotFound) {
		return nil, fmt.Errorf("no account is %q", ref)
	}
	return found, err
}

func describeAccount(u *db.User) string {
	if u.Type == db.UserTypeWikidot {
		return fmt.Sprintf("wd:%s (#%d)", u.WikidotUsername, u.ID)
	}
	return fmt.Sprintf("%s (#%d)", u.Username, u.ID)
}

func confirmMerge(in *bufio.Reader, source, target *db.User) error {
	fmt.Fprintf(os.Stderr, `Everything %s holds on every site moves to %s:
pages it wrote, revisions, votes, forum posts, roles, messages and notifications.
%s is then deleted. This cannot be undone; take a backup first.

Merge? [y/N] `, describeAccount(source), describeAccount(target), describeAccount(source))
	answer, err := in.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return errors.New("cancelled")
	}
	return nil
}

const pruneSample = 20

func pruneUsers(database, dataDir string, apply, yes bool) error {
	ctx := context.Background()
	dsn, release, err := resolveDatabase(ctx, database, dataDir)
	if err != nil {
		return err
	}
	defer release()
	conn, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close()

	candidates, err := conn.UnreferencedWikidotUsers(ctx)
	if err != nil {
		return err
	}
	mentions, err := conn.UserMentions(ctx)
	if err != nil {
		return err
	}
	unused, named := splitMentioned(candidates, mentions)

	for i, u := range unused {
		if i == pruneSample {
			fmt.Printf("  ... and %d more\n", len(unused)-pruneSample)
			break
		}
		fmt.Printf("  wd:%s (#%d)\n", u.WikidotUsername, u.ID)
	}
	fmt.Printf("%d imported accounts are not used by any site", len(unused))
	if named > 0 {
		fmt.Printf("; %d more are kept because a page or post names them", named)
	}
	fmt.Println()
	if len(unused) == 0 {
		return nil
	}
	if !apply {
		fmt.Println("nothing was deleted; run again with -apply to delete them")
		return nil
	}
	if !yes {
		if err := confirmPrune(bufio.NewReader(os.Stdin), len(unused)); err != nil {
			return err
		}
	}

	ids := make([]int64, len(unused))
	for i, u := range unused {
		ids[i] = u.ID
	}
	deleted, err := conn.DeleteUnreferencedWikidotUsers(ctx, ids)
	if err != nil {
		return err
	}
	fmt.Printf("deleted %d accounts\n", deleted)
	return nil
}

func splitMentioned(candidates []db.UnusedUser, mentions []string) ([]db.UnusedUser, int) {
	names := make(map[string]bool, 2*len(mentions))
	for _, m := range mentions {
		m = strings.TrimSpace(m)
		if len(m) >= 3 && strings.EqualFold(m[:3], "wd:") {
			m = m[3:]
		}
		names[strings.ToLower(m)] = true
		names[wikidot.CanonicalizeUsername(m)] = true
	}
	var unused []db.UnusedUser
	named := 0
	for _, u := range candidates {
		if names[strings.ToLower(u.WikidotUsername)] ||
			names[wikidot.CanonicalizeUsername(u.WikidotUsername)] ||
			(u.DisplayName != "" && names[strings.ToLower(u.DisplayName)]) {
			named++
			continue
		}
		unused = append(unused, u)
	}
	return unused, named
}

func confirmPrune(in *bufio.Reader, count int) error {
	fmt.Fprintf(os.Stderr, `%d imported accounts are deleted. They cannot be claimed afterwards,
and importing a backup that names them creates them again. This cannot be undone;
take a backup first.

Delete? [y/N] `, count)
	answer, err := in.ReadString('\n')
	if err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return errors.New("cancelled")
	}
	return nil
}
