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

  merge  move everything one account holds onto another, then delete the first

Name an account by its user name, by wd:<name> for the name it had on Wikidot,
or by #<number>. Quote #<number> in a shell, or it is read as a comment:

  pwikit user merge -from '#3404' -into '#121'

Options:
  -from      account to merge away
  -into      account that keeps everything
  -yes       merge without asking
  -database  PostgreSQL connection string
  -data-dir  state directory; defaults to the directory holding the executable
`)
}

func userCommand(args []string) error {
	if len(args) == 0 || args[0] != "merge" {
		userUsage()
		return errors.New("unknown user subcommand")
	}
	fs := flag.NewFlagSet("user merge", flag.ContinueOnError)
	fs.Usage = userUsage
	from := fs.String("from", "", "account to merge away")
	into := fs.String("into", "", "account that keeps everything")
	yes := fs.Bool("yes", false, "merge without asking")
	database := fs.String("database", os.Getenv(envDatabase), "PostgreSQL connection string")
	dataDir := fs.String("data-dir", "", "state directory; defaults to the directory holding the executable")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
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
