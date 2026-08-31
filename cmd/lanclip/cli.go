package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dpellerin/lanclip/internal/clipboard"
	"github.com/dpellerin/lanclip/internal/config"
	"github.com/dpellerin/lanclip/internal/control"
	"github.com/dpellerin/lanclip/internal/daemon"
	"github.com/dpellerin/lanclip/internal/pairing"
)

type controlCaller func(context.Context, string, control.Request) (control.Response, error)

func runCLI(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help")) {
		printHelp(output)
		return nil
	}
	if args[0] == "version" {
		if len(args) != 1 {
			return usageError()
		}
		fmt.Fprintf(output, "Lanclip %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return nil
	}
	if args[0] == "clipboard-event" {
		if len(args) != 2 {
			return errors.New("invalid internal callback")
		}
		return clipboard.SendWatchEvent(args[1], input, config.MaxPayload)
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if args[0] == "daemon" {
		if len(args) != 1 {
			return usageError()
		}
		return runDaemon(paths)
	}
	return runControl(args, paths.Socket, bufio.NewReader(input), output, control.Call)
}

func runControl(args []string, socket string, input *bufio.Reader, output io.Writer, call controlCaller) error {
	command := args[0]
	actions := map[string]bool{"pair": true, "approve": true, "reject": true, "unpair": true}
	reports := map[string]bool{"status": true, "peers": true, "doctor": true}
	if !actions[command] && !reports[command] {
		return usageError()
	}
	if reports[command] && len(args) != 1 || actions[command] && len(args) > 2 {
		return usageError()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request := func(command, argument string) (control.Response, error) {
		resp, err := call(ctx, socket, control.Request{Command: command, Argument: argument})
		if err != nil {
			return control.Response{}, fmt.Errorf("Lanclip service is unavailable: %w", err)
		}
		if !resp.OK {
			return control.Response{}, errors.New(resp.Error)
		}
		return resp, nil
	}

	if reports[command] {
		resp, err := request(command, "")
		if err != nil {
			return err
		}
		switch command {
		case "status":
			var status daemon.Status
			if err := decode(resp.Data, &status); err != nil {
				return err
			}
			printStatus(output, status)
		case "peers":
			peers, err := decodePeers(resp.Data)
			if err != nil {
				return err
			}
			printPeers(output, peers)
		case "doctor":
			var checks []daemon.DoctorCheck
			if err := decode(resp.Data, &checks); err != nil {
				return err
			}
			printDoctor(output, checks)
		}
		return nil
	}

	peersResp, err := request("peers", "")
	if err != nil {
		return err
	}
	peers, err := decodePeers(peersResp.Data)
	if err != nil {
		return err
	}
	candidates := eligiblePeers(command, peers)
	var selected daemon.PeerStatus
	var found bool
	if len(args) == 2 {
		selected, found = findPeer(candidates, args[1])
		if !found {
			return fmt.Errorf("no device named %q is available to %s; run 'lanclip peers' to see available devices", args[1], command)
		}
	} else {
		selected, err = choosePeer(input, output, command, candidates)
		if err != nil {
			return err
		}
	}

	if command == "approve" {
		printPairingIdentity(output, selected)
		ok, err := confirm(input, output, "Do the code and fingerprint match on both devices?", false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(output, "Approval cancelled. No trust was added.")
			return nil
		}
	}
	if command == "reject" || command == "unpair" {
		verb := "Reject"
		if command == "unpair" {
			verb = "Remove"
		}
		ok, err := confirm(input, output, fmt.Sprintf("%s %s?", verb, selected.Name), false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(output, "Cancelled. No changes were made.")
			return nil
		}
	}

	// Resolve the friendly name or menu choice locally, then send the stable ID
	// over the private control socket. Users never need to see or type it.
	resp, err := request(command, selected.ID)
	if err != nil {
		return err
	}
	if command == "pair" {
		var peer pairing.Peer
		if err := decode(resp.Data, &peer); err != nil {
			return err
		}
		fmt.Fprintf(output, "Pairing request sent to %s.\n\n", peer.Name)
		printPairingIdentity(output, daemon.PeerStatus{Peer: peer})
		fmt.Fprintf(output, "If both match, run this on each device:\n  lanclip approve %q\n", peer.Name)
		return nil
	}
	messages := map[string]string{
		"approve": fmt.Sprintf("Approved %s. Clipboard sync will connect automatically.", selected.Name),
		"reject":  fmt.Sprintf("Rejected %s.", selected.Name),
		"unpair":  fmt.Sprintf("Removed %s. Run 'lanclip pair' to pair it again.", selected.Name),
	}
	fmt.Fprintln(output, messages[command])
	return nil
}

func decode(data any, target any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("read service response: %w", err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("read service response: %w", err)
	}
	return nil
}

func decodePeers(data any) ([]daemon.PeerStatus, error) {
	var peers []daemon.PeerStatus
	if err := decode(data, &peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func eligiblePeers(command string, peers []daemon.PeerStatus) []daemon.PeerStatus {
	out := make([]daemon.PeerStatus, 0, len(peers))
	for _, peer := range peers {
		switch command {
		case "pair":
			if (peer.State == "unpaired" || peer.State == pairing.Rejected) && len(peer.Addresses) > 0 {
				out = append(out, peer)
			}
		case "approve", "reject":
			if peer.State == pairing.Pending {
				out = append(out, peer)
			}
		case "unpair":
			if peer.State == pairing.Trusted {
				out = append(out, peer)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func choosePeer(input *bufio.Reader, output io.Writer, action string, peers []daemon.PeerStatus) (daemon.PeerStatus, error) {
	if len(peers) == 0 {
		messages := map[string]string{
			"pair":    "No unpaired devices are available. Make sure the other device is running on this LAN.",
			"approve": "No devices are waiting for approval.",
			"reject":  "No devices are waiting for approval.",
			"unpair":  "No paired devices are available to remove.",
		}
		return daemon.PeerStatus{}, errors.New(messages[action])
	}
	fmt.Fprintf(output, "Choose a device to %s:\n", action)
	for i, peer := range peers {
		fmt.Fprintf(output, "  %d) %s\n", i+1, peer.Name)
	}
	for {
		if len(peers) == 1 {
			fmt.Fprint(output, "Selection [1]: ")
		} else {
			fmt.Fprintf(output, "Selection [1-%d]: ", len(peers))
		}
		line, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return daemon.PeerStatus{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" && len(peers) == 1 {
			return peers[0], nil
		}
		n, convErr := strconv.Atoi(line)
		if convErr == nil && n >= 1 && n <= len(peers) {
			return peers[n-1], nil
		}
		if errors.Is(err, io.EOF) {
			return daemon.PeerStatus{}, errors.New("no selection entered")
		}
		fmt.Fprintln(output, "Enter the number shown next to the device.")
	}
}

func findPeer(peers []daemon.PeerStatus, query string) (daemon.PeerStatus, bool) {
	query = strings.TrimSpace(query)
	matches := []daemon.PeerStatus{}
	for _, peer := range peers {
		if strings.EqualFold(peer.Name, query) || strings.HasPrefix(peer.ID, query) {
			matches = append(matches, peer)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return daemon.PeerStatus{}, false
}

func confirm(input *bufio.Reader, output io.Writer, question string, defaultYes bool) (bool, error) {
	prompt := " [y/N]: "
	if defaultYes {
		prompt = " [Y/n]: "
	}
	for {
		fmt.Fprint(output, question+prompt)
		line, err := input.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "" {
			return defaultYes, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
		if answer == "n" || answer == "no" {
			return false, nil
		}
		if errors.Is(err, io.EOF) {
			return false, errors.New("no confirmation entered")
		}
		fmt.Fprintln(output, "Please enter y or n.")
	}
}

func printStatus(output io.Writer, status daemon.Status) {
	fmt.Fprintf(output, "Lanclip %s\n\n", status.Version)
	fmt.Fprintf(output, "This device:  %s\n", status.Name)
	fmt.Fprintf(output, "Service:      running for %s\n", status.Uptime)
	clipState := "ready"
	if !status.Clipboard.Running {
		clipState = "needs attention"
	}
	if status.Clipboard.LastError != "" {
		clipState += " — " + status.Clipboard.LastError
	}
	fmt.Fprintf(output, "Clipboard:    %s", clipState)
	if status.Clipboard.LastEvent != "" {
		fmt.Fprintf(output, " (last activity %s)", friendlyTime(status.Clipboard.LastEvent))
	}
	fmt.Fprintln(output)
	seen := int(mapNumber(status.Discovery, "peers_seen"))
	discovery := "healthy"
	if e := mapString(status.Discovery, "last_error"); e != "" {
		discovery = "needs attention — " + e
	}
	fmt.Fprintf(output, "Discovery:    %s (%s found", discovery, plural(seen, "device", "devices"))
	if last := mapString(status.Discovery, "last_browse"); last != "" {
		fmt.Fprintf(output, ", checked %s", friendlyTime(last))
	}
	fmt.Fprintln(output, ")")

	trusted := make([]daemon.PeerStatus, 0, len(status.Peers))
	for _, peer := range status.Peers {
		if peer.State == pairing.Trusted {
			trusted = append(trusted, peer)
		}
	}
	fmt.Fprintln(output, "\nPaired devices:")
	if len(trusted) == 0 {
		fmt.Fprintln(output, "  None. Run 'lanclip pair' to add one.")
		return
	}
	for _, peer := range trusted {
		state := "offline"
		if peer.Stats.Connected {
			state = "connected"
		}
		fmt.Fprintf(output, "  %s — %s\n", peer.Name, state)
		if peer.Stats.Address != "" {
			fmt.Fprintf(output, "    Address:       %s\n", peer.Stats.Address)
		} else if len(peer.Addresses) > 0 {
			fmt.Fprintf(output, "    Address:       %s\n", peer.Addresses[0])
		}
		if !peer.Stats.LastSent.IsZero() {
			fmt.Fprintf(output, "    Last sent:     %s (%s)\n", friendlyTime(peer.Stats.LastSent.Format(time.RFC3339Nano)), byteCount(peer.Stats.SentBytes))
		}
		if !peer.Stats.LastReceived.IsZero() {
			fmt.Fprintf(output, "    Last received: %s (%s)\n", friendlyTime(peer.Stats.LastReceived.Format(time.RFC3339Nano)), byteCount(peer.Stats.ReceivedBytes))
		}
		fmt.Fprintf(output, "    Reconnects:    %d\n", peer.Stats.Reconnects)
		if peer.Stats.LastError != "" && !peer.Stats.Connected {
			fmt.Fprintf(output, "    Last error:    %s\n", peer.Stats.LastError)
		}
	}
}

func printPeers(output io.Writer, peers []daemon.PeerStatus) {
	fmt.Fprintln(output, "Devices:")
	if len(peers) == 0 {
		fmt.Fprintln(output, "  No devices found. Make sure Lanclip is running on the other device.")
		return
	}
	for _, peer := range peers {
		state := humanPeerState(peer)
		fmt.Fprintf(output, "  %s — %s\n", peer.Name, state)
		if peer.State == pairing.Pending {
			fmt.Fprintf(output, "    Code:        %s\n", peer.ComparisonCode)
			fmt.Fprintf(output, "    Fingerprint: %s\n", groupedFingerprint(peer.Fingerprint))
			fmt.Fprintf(output, "    Next:        lanclip approve %q\n", peer.Name)
		} else if peer.Stats.Address != "" {
			fmt.Fprintf(output, "    Address:     %s\n", peer.Stats.Address)
		} else if len(peer.Addresses) > 0 {
			fmt.Fprintf(output, "    Address:     %s\n", peer.Addresses[0])
		}
	}
}

func printDoctor(output io.Writer, checks []daemon.DoctorCheck) {
	fmt.Fprintln(output, "Lanclip doctor:")
	passed := 0
	for _, check := range checks {
		mark := "[ok]"
		if check.OK {
			passed++
		} else {
			mark = "[!!]"
		}
		fmt.Fprintf(output, "  %s %s", mark, check.Name)
		if check.Detail != "" {
			fmt.Fprintf(output, " — %s", check.Detail)
		}
		fmt.Fprintln(output)
	}
	if passed == len(checks) {
		fmt.Fprintf(output, "\nAll %d checks passed.\n", passed)
	} else {
		fmt.Fprintf(output, "\n%d of %d checks passed; review the items marked [!!].\n", passed, len(checks))
	}
}

func printPairingIdentity(output io.Writer, peer daemon.PeerStatus) {
	fmt.Fprintf(output, "Compare these on both devices:\n  Code:        %s\n  Fingerprint: %s\n\n", peer.ComparisonCode, groupedFingerprint(peer.Fingerprint))
}

func humanPeerState(peer daemon.PeerStatus) string {
	switch peer.State {
	case pairing.Trusted:
		if peer.Stats.Connected {
			return "paired and connected"
		}
		return "paired, currently offline"
	case pairing.Pending:
		return "waiting for approval"
	case pairing.Rejected:
		return "rejected"
	default:
		return "available to pair"
	}
}

func friendlyTime(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	if d < 10*time.Second {
		return "just now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return t.Local().Format("Jan 2 at 3:04 PM")
}

func groupedFingerprint(value string) string {
	groups := make([]string, 0, (len(value)+3)/4)
	for len(value) > 4 {
		groups = append(groups, value[:4])
		value = value[4:]
	}
	if value != "" {
		groups = append(groups, value)
	}
	return strings.Join(groups, " ")
}

func mapString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func mapNumber(values map[string]any, key string) float64 {
	switch value := values[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return 0
	}
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func byteCount(n int) string {
	return plural(n, "byte", "bytes")
}

func printHelp(output io.Writer) {
	fmt.Fprintln(output, `Lanclip — private clipboard sync for your local network

Usage:
  lanclip status              Show service and connection health
  lanclip peers               List nearby and paired devices
  lanclip pair [device]       Pair with a nearby device
  lanclip approve [device]    Approve a matching pairing request
  lanclip reject [device]     Reject a pairing request
  lanclip unpair [device]     Remove a paired device
  lanclip doctor              Check the local setup
  lanclip version             Show the installed version

Device names are optional. Without one, Lanclip presents a numbered menu.`)
}

func usageError() error {
	return errors.New("unknown or invalid command; run 'lanclip help' for usage")
}
