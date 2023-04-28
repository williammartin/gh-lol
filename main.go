package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/config"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/cli/go-gh/v2/pkg/tableprinter"
	"github.com/cli/go-gh/v2/pkg/term"
	graphql "github.com/cli/shurcooL-graphql"
	"github.com/spf13/cobra"
)

func main() {
	if err := _main(); err != nil {
		fmt.Fprintf(os.Stderr, "X %s", err.Error())
	}
}

func _main() error {
	cfg, err := config.Read()
	if err != nil {
		return err
	}

	supportive, _ := cfg.Get([]string{"supportive"})

	var repo repository.Repository

	rootCmd := &cobra.Command{
		Use:   "lol <subcommand> [flags]",
		Short: "gh lol",
	}

	repoOverride := rootCmd.PersistentFlags().StringP("repo", "R", "", "Repository to use in OWNER/REPO format")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) (err error) {
		if supportive == "enabled" {
			fmt.Println("hey. you're doing great. take a break if you need to.")
		}

		if *repoOverride != "" {
			repo, err = repository.Parse(*repoOverride)
		} else {
			repo, err = repository.Current()
		}
		return
	}

	spamCmd := &cobra.Command{
		Use:   "spam [<message>]",
		Short: "Comment on a random issue or pr in a repository",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			return runSpam(cmd, repo, args)
		},
	}

	yellCmd := &cobra.Command{
		Use:   "yell",
		Short: "Print a list of issues loudly",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			return runYell(cmd, repo)
		},
	}

	yellCmd.Flags().IntP("loud", "l", 0, "how loud to be")

	rootCmd.AddCommand(spamCmd)
	rootCmd.AddCommand(yellCmd)

	return rootCmd.Execute()
}

func runYell(cmd *cobra.Command, repo repository.Repository) (err error) {
	var loud int
	loudFlag := cmd.Flags().Lookup("loud")
	if !loudFlag.Changed {
		terminal := term.FromEnv()
		if !terminal.IsTerminalOutput() {
			return errors.New("--loud required when not running interactively")
		}
		if err = survey.AskOne(&survey.Input{
			Message: "How loud?",
			Default: "1",
		}, &loud); err != nil {
			return
		}
	} else {
		loud, _ = cmd.Flags().GetInt("loud")
	}

	if loud < 1 {
		return fmt.Errorf("expected a loudness of at least 1, got '%d'", loud)
	}

	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return fmt.Errorf("couldn't make client: %w", err)
	}

	var query struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					Title  string
					Number int
				}
			} `graphql:"issues(first: 25, states: [OPEN])"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	variables := map[string]interface{}{
		"owner": graphql.String(repo.Owner),
		"name":  graphql.String(repo.Name),
	}

	if err = client.Query("Issues", &query, variables); err != nil {
		return fmt.Errorf("failed to call API: %w", err)
	}
	for _, i := range query.Repository.Issues.Nodes {
		fmt.Println(i.Number, i.Title)
	}

	terminal := term.FromEnv()
	termWidth, _, _ := terminal.Size()
	tp := tableprinter.New(terminal.Out(), terminal.IsTerminalOutput(), termWidth)

	cyan := func(s string) string {
		return "\x1b[36m" + s + "\x1b[m"
	}

	for _, i := range query.Repository.Issues.Nodes {
		tp.AddField(fmt.Sprintf("#%d", i.Number), tableprinter.WithColor(cyan))
		title := strings.ToUpper(i.Title)
		for x := 0; x < loud; x++ {
			title += "!"
		}
		tp.AddField(title)
		tp.EndRow()
	}

	return tp.Render()
}

func runSpam(cmd *cobra.Command, repo repository.Repository, args []string) (err error) {
	var message string
	if len(args) > 0 {
		message = args[0]
	} else {
		terminal := term.FromEnv()
		if !terminal.IsTerminalOutput() {
			return errors.New("--loud required when not running interactively")
		}
		if err = survey.AskOne(&survey.Input{
			Message: "Comment",
		}, &message); err != nil {
			return
		}
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		return fmt.Errorf("couldn't create client: %w", err)
	}

	path := fmt.Sprintf("repos/%s/%s/issues?per_page=100", repo.Owner, repo.Name)

	response := []struct {
		Number int
	}{}

	err = client.Get(path, &response)
	if err != nil {
		return fmt.Errorf("failed to get API: %w", err)
	}

	if len(response) == 0 {
		return fmt.Errorf("no issues to choose from in %s/%s", repo.Owner, repo.Name)
	}

	rand.Seed(time.Now().Unix())
	choice := response[rand.Intn(len(response))].Number

	path = fmt.Sprintf("repos/%s/%s/issues/%d/comments", repo.Owner, repo.Name, choice)

	body := map[string]string{
		"body": message,
	}

	bodyJSON, _ := json.Marshal(body)

	if err = client.Post(path, bytes.NewReader(bodyJSON), nil); err != nil {
		return fmt.Errorf("failed to post API: %w", err)
	}

	path = fmt.Sprintf("repos/%s/%s/issues/%d", repo.Owner, repo.Name, choice)

	body = map[string]string{
		"state": "closed",
	}

	bodyJSON, _ = json.Marshal(body)

	if err = client.Patch(path, bytes.NewReader(bodyJSON), nil); err != nil {
		return fmt.Errorf("failed to patch API: %w", err)
	}

	fmt.Printf("Closed #%d with '%s'\n", choice, message)

	return
}
