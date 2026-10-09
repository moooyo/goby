package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

type auxiliaryProbeFacts struct {
	Stored                       storedFile
	Probe                        *media.Info
	Row                          rootBindingRow
	Candidate                    themeCandidate
	ID, Name, SortName, ItemType string
}

func preparedAuxiliaryFacts(file *preparedThemeFile) auxiliaryProbeFacts {
	return auxiliaryProbeFacts{file.input.stored, file.input.probe, file.sourceRow,
		file.candidate, file.id, file.name, file.sortName, file.itemType}
}

// The synthetic full-audit slice has cap=len, independently of the retained
// files slice's capacity. Charge its header once and each value's inline and
// dynamic storage once. Nested capacities and shared pointers keep their
// existing conservative charges; this is not a heap-identity cache.
type auxiliaryProbeFactsBudget struct{ remaining int64 }

func newAuxiliaryProbeFactsBudget(limit int64) auxiliaryProbeFactsBudget {
	header, fits := scanProbeFactsSize([]auxiliaryProbeFacts(nil), limit)
	if !fits {
		return auxiliaryProbeFactsBudget{remaining: -1}
	}
	return auxiliaryProbeFactsBudget{remaining: limit - header}
}

func (budget *auxiliaryProbeFactsBudget) add(file *preparedThemeFile) bool {
	charge, fits := scanProbeFactsSize(preparedAuxiliaryFacts(file), budget.remaining)
	if fits {
		budget.remaining -= charge
	}
	return fits
}

func (budget *auxiliaryProbeFactsBudget) addSortName(name string) bool {
	if budget.remaining < int64(len(name)) {
		return false
	}
	budget.remaining -= int64(len(name))
	return true
}

func (state *scanState) prepareAuxiliarySortNames(files []*preparedThemeFile, budget *auxiliaryProbeFactsBudget) (resultErr error) {
	if len(files) == 0 {
		return nil
	}
	names := make([]string, len(files))
	for index, file := range files {
		names[index] = file.name
	}
	// Every input descriptor is already retired. This statement captures one
	// current settings row for all names, retaining duplicates and source case.
	// Publication still derives authoritative sorting in its own transaction.
	rows, err := state.store.pool.Query(state.task.ctx, `SELECT names.ordinal,
		goby_generated_sort_name(names.name,settings.sort_remove_words,true)
		FROM unnest($1::text[]) WITH ORDINALITY AS names(name,ordinal)
		CROSS JOIN managed_settings settings WHERE settings.id=1 ORDER BY names.ordinal`, names)
	if err != nil {
		return err
	}
	defer func() {
		rows.Close()
		if closeErr := rows.Err(); closeErr != nil && !errors.Is(resultErr, closeErr) {
			resultErr = errors.Join(resultErr, closeErr)
		}
	}()
	return applyAuxiliarySortNames(state.task.ctx, files, rows, budget)
}

func applyAuxiliarySortNames(ctx context.Context, files []*preparedThemeFile, rows OwnedRows, budget *auxiliaryProbeFactsBudget) error {
	count := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var ordinal int64
		var name string
		if err := rows.Scan(&ordinal, &name); err != nil {
			return err
		}
		if count >= len(files) || ordinal != int64(count+1) {
			return fmt.Errorf("auxiliary sort-name query returned an invalid ordinal")
		}
		if !budget.addSortName(name) {
			return errScanProbeFactsBudget
		}
		file := files[count]
		file.sortName = name
		file.changed = auxiliaryPreparedFileChanged(file)
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if count != len(files) {
		if count == 0 {
			return pgx.ErrNoRows
		}
		return fmt.Errorf("auxiliary sort-name query returned an incomplete group")
	}
	return nil
}

func auxiliaryPreparedFileChanged(file *preparedThemeFile) bool {
	input, stored := file.input, file.input.stored
	previousName, previousSort, previousOverview := stored.name, stored.sortName, stored.overview
	if stored.automatic != nil {
		previousName, previousSort, previousOverview = stored.automatic.Name, stored.automatic.SortName, stored.automatic.Overview
	}
	return !input.unchanged || stored.id == "" || stored.rootID != file.state.root.id ||
		stored.path != filepath.Join(file.state.root.path, filepath.FromSlash(file.candidate.relative)) || stored.parentID != file.owner.id ||
		stored.itemType != file.itemType || previousName != file.name || previousSort != file.sortName || previousOverview != "" ||
		stored.indexNumber != 0 || stored.parentIndexNumber != 0 || !reflect.DeepEqual(stored.local, localMetadata{})
}
