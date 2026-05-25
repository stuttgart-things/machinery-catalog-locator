package grpcserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/stuttgart-things/machinery-catalog-locator/catalogservice"
	"github.com/stuttgart-things/machinery-catalog-locator/internal/catalog"
)

const (
	watchDefaultInterval = 30 * time.Second
	watchMinInterval     = 5 * time.Second
	watchMaxInterval     = time.Hour
)

// WatchTree replays the current snapshot as ADDED events, then on each
// tick re-resolves the tree, diffs against the previous snapshot, and
// emits ADDED/MODIFIED/DELETED events. The stream ends when the client
// disconnects or the resolve fails twice in a row (so a transient GitHub
// outage doesn't tear down every watcher).
func (s *Server) WatchTree(req *catalogservice.WatchTreeRequest, stream catalogservice.CatalogService_WatchTreeServer) error {
	root, err := catalog.ParseBlobURL(req.GetRootUrl())
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "root_url: %v", err)
	}

	interval := time.Duration(req.GetIntervalSeconds()) * time.Second
	switch {
	case interval == 0:
		interval = watchDefaultInterval
	case interval < watchMinInterval:
		interval = watchMinInterval
	case interval > watchMaxInterval:
		interval = watchMaxInterval
	}

	ctx := stream.Context()
	prev, err := s.snapshot(ctx, root)
	if err != nil {
		return status.Errorf(codes.Unavailable, "initial resolve: %v", err)
	}
	for _, e := range prev.asAdded() {
		if err := stream.Send(e); err != nil {
			return err
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			next, err := s.snapshot(ctx, root)
			if err != nil {
				consecutiveFailures++
				slog.Warn("watch resolve failed", "root", req.GetRootUrl(), "attempt", consecutiveFailures, "err", err)
				if consecutiveFailures >= 2 {
					return status.Errorf(codes.Unavailable, "resolve failed twice: %v", err)
				}
				continue
			}
			consecutiveFailures = 0
			for _, ev := range prev.diff(next) {
				if err := stream.Send(ev); err != nil {
					return err
				}
			}
			prev = next
		}
	}
}

// snapshot resolves the tree and indexes every node by a stable key so
// successive snapshots can be diffed cheaply.
func (s *Server) snapshot(ctx context.Context, root catalog.SourceRef) (treeSnapshot, error) {
	nodes, err := s.Resolver.Resolve(ctx, root)
	if err != nil {
		return treeSnapshot{}, err
	}
	snap := treeSnapshot{nodes: map[string]*catalog.Node{}, hashes: map[string]string{}}
	for _, n := range catalog.Flatten(nodes) {
		k := nodeKey(n)
		snap.nodes[k] = n
		snap.hashes[k] = hashEntity(n)
	}
	return snap, nil
}

type treeSnapshot struct {
	nodes  map[string]*catalog.Node
	hashes map[string]string
}

func (s treeSnapshot) asAdded() []*catalogservice.TreeEvent {
	events := make([]*catalogservice.TreeEvent, 0, len(s.nodes))
	for _, n := range s.nodes {
		events = append(events, &catalogservice.TreeEvent{
			Type: catalogservice.EventType_ADDED,
			Node: toProtoNode(n),
		})
	}
	return events
}

// diff emits one event per change between this snapshot (the previous
// one) and next. MODIFIED is decided by content hash on the entity
// itself; tree-shape changes (a node moving to a different parent) are
// surfaced as MODIFIED too because the key includes the source path.
func (s treeSnapshot) diff(next treeSnapshot) []*catalogservice.TreeEvent {
	var events []*catalogservice.TreeEvent
	for k, n := range next.nodes {
		if _, existed := s.nodes[k]; !existed {
			events = append(events, &catalogservice.TreeEvent{
				Type: catalogservice.EventType_ADDED, Node: toProtoNode(n),
			})
			continue
		}
		if s.hashes[k] != next.hashes[k] {
			events = append(events, &catalogservice.TreeEvent{
				Type: catalogservice.EventType_MODIFIED, Node: toProtoNode(n),
			})
		}
	}
	for k, n := range s.nodes {
		if _, kept := next.nodes[k]; !kept {
			events = append(events, &catalogservice.TreeEvent{
				Type: catalogservice.EventType_DELETED, Node: toProtoNode(n),
			})
		}
	}
	return events
}

// nodeKey is the identity used for diffing across snapshots. Two nodes
// in the same file with the same kind/namespace/name are considered the
// same logical entity even if their DocIndex shifts after an edit.
func nodeKey(n *catalog.Node) string {
	ns := n.Entity.Metadata.Namespace
	if ns == "" {
		ns = "default"
	}
	return fmt.Sprintf("%s|%s/%s/%s",
		n.Source.String(), n.Entity.Kind, ns, n.Entity.Metadata.Name)
}

func hashEntity(n *catalog.Node) string {
	b, _ := json.Marshal(n.Entity)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
