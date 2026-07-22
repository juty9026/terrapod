package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/juty9026/terrapod/internal/model"
	"github.com/juty9026/terrapod/internal/reconcile"
	"github.com/juty9026/terrapod/internal/resolve"
)

func renderHelp(output io.Writer) {
	fmt.Fprintln(output, "Terrapod — Personal Development Environment Manager")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  apply      Reconcile package resources")
	fmt.Fprintln(output, "  resolve    Confirm unmanaged blockers for one resource")
	fmt.Fprintln(output, "  plan       Show deterministic reconciliation operations")
	fmt.Fprintln(output, "  status     Show Ready and Unavailable resources")
	fmt.Fprintln(output, "  doctor     Check whether enabled resources are available")
	fmt.Fprintln(output, "  diff       Report managed-file shadow status")
	fmt.Fprintln(output, "  version    Show the development version")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Mutation commands:")
	for _, command := range []string{"update", "setup", "configure", "chezmoi"} {
		fmt.Fprintf(output, "  %s (unavailable until activation)\n", command)
	}
}

func renderResolveResult(output io.Writer, result resolve.Result) {
	if !result.Proceeded {
		return
	}
	fmt.Fprintln(output)
	renderApplySummary(output, result.Summary)
}

func renderApplySummary(output io.Writer, summary reconcile.Summary) {
	ready := append([]model.ResourceID(nil), summary.Ready...)
	sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })
	fmt.Fprintln(output, "Ready:")
	if len(ready) == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	for _, id := range ready {
		fmt.Fprintf(output, "  %s\n", id)
	}
	fmt.Fprintln(output, "Unavailable:")
	ids := sortedUnavailableIDs(summary.Unavailable)
	if len(ids) == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	for _, id := range ids {
		fmt.Fprintf(output, "  %s: %s\n", id, summary.Unavailable[id])
	}
}

func renderPlan(output io.Writer, plan model.Plan, lock string) {
	fmt.Fprintf(output, "Release: %s\n", plan.Release)
	fmt.Fprintf(output, "Reconciliation lock: %s\n", lock)
	sections := []struct {
		name string
		kind model.OperationKind
	}{
		{"Adopt", model.OperationAdopt},
		{"Install", model.OperationInstall},
		{"Upgrade", model.OperationUpgrade},
		{"Transfer", model.OperationTransfer},
		{"Prune", model.OperationPrune},
	}
	for _, section := range sections {
		fmt.Fprintf(output, "%s:\n", section.name)
		operations := operationsOfKind(plan.Operations, section.kind)
		if len(operations) == 0 {
			fmt.Fprintln(output, "  (none)")
		}
		for _, operation := range operations {
			detail := operation.Detail
			if detail == "" {
				detail = operation.ID
			}
			fmt.Fprintf(output, "  %s: %s\n", operation.ResourceID, detail)
		}
	}
	fmt.Fprintln(output, "Unavailable:")
	ids := sortedUnavailableIDs(plan.Unavailable)
	if len(ids) == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	for _, id := range ids {
		fmt.Fprintf(output, "  %s: %s\n", id, plan.Unavailable[id])
	}
}

func renderStatus(output io.Writer, snapshot reconciliation) {
	fmt.Fprintf(output, "Release: %s\n", snapshot.plan.Release)
	fmt.Fprintf(output, "Reconciliation lock: %s\n", snapshot.lock)
	for _, status := range resourceStatuses(snapshot) {
		fmt.Fprintf(output, "%s: %s\n", status.id, status.value)
	}
}

func renderDoctor(output io.Writer, snapshot reconciliation) bool {
	fmt.Fprintf(output, "Reconciliation lock: %s\n", snapshot.lock)
	unavailable := false
	for _, status := range resourceStatuses(snapshot) {
		fmt.Fprintf(output, "%s: %s\n", status.id, status.value)
		if strings.HasPrefix(status.value, "Unavailable") {
			unavailable = true
		}
	}
	return unavailable
}

type renderedStatus struct {
	id    model.ResourceID
	value string
}

func resourceStatuses(snapshot reconciliation) []renderedStatus {
	pending := make(map[model.ResourceID][]model.OperationKind)
	for _, operation := range snapshot.plan.Operations {
		pending[operation.ResourceID] = append(pending[operation.ResourceID], operation.Kind)
	}
	statuses := make([]renderedStatus, 0)
	for _, resource := range enabledResources(snapshot.catalog, snapshot.config) {
		if reason, unavailable := snapshot.plan.Unavailable[resource.ID]; unavailable {
			statuses = append(statuses, renderedStatus{resource.ID, "Unavailable (" + reason + ")"})
			continue
		}
		kinds := pending[resource.ID]
		if len(kinds) != 0 {
			names := make([]string, len(kinds))
			for i, kind := range kinds {
				names[i] = string(kind)
			}
			sort.Strings(names)
			statuses = append(statuses, renderedStatus{resource.ID, "Unavailable (pending " + strings.Join(names, ", ") + ")"})
			continue
		}
		statuses = append(statuses, renderedStatus{resource.ID, "Ready"})
	}
	return statuses
}

func operationsOfKind(operations []model.Operation, kind model.OperationKind) []model.Operation {
	selected := make([]model.Operation, 0)
	for _, operation := range operations {
		if operation.Kind == kind {
			selected = append(selected, operation)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].ResourceID != selected[j].ResourceID {
			return selected[i].ResourceID < selected[j].ResourceID
		}
		return selected[i].ID < selected[j].ID
	})
	return selected
}

func sortedUnavailableIDs(unavailable map[model.ResourceID]string) []model.ResourceID {
	ids := make([]model.ResourceID, 0, len(unavailable))
	for id := range unavailable {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
