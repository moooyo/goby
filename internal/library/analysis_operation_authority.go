package library

import (
	"context"
	"errors"
	"strconv"
)

// BeginAnalysisOperation checks the admitted sources before retaining the exact
// task's root approval. Only this process-local context carries the approval;
// future executions and callers without it use current authorization metadata.
func (s *Store) BeginAnalysisOperation(ctx context.Context, childID string, fence AnalysisFence) (resultContext context.Context, closeResult func() error, resultErr error) {
	if _, err := s.GetAnalysisWork(ctx, childID, fence); err != nil {
		return nil, nil, err
	}
	work, closeOperation, err := s.BeginTaskSourceOperation(ctx, childID, fence)
	if err != nil {
		return nil, nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			resultErr = errors.Join(resultErr, closeOperation())
		}
	}()
	// A grant captured after the first source read must still describe the
	// admitted work. Mapping, source, manual and cohort changes remain fences.
	if _, err := s.GetAnalysisWork(work, childID, fence); err != nil {
		return nil, nil, err
	}
	handedOff = true
	return work, closeOperation, nil
}

// The public source stamp and cache protocol remain unchanged. Internal work
// substitutes only the binding revision approved at its execution start, while
// recomputing every physical file and probe fact from the current catalog.
// Values use a bound JSON parameter; the generated placeholder is an argument
// index, never caller-supplied SQL. Unknown roots cannot inherit an approval.
func analysisOperationSourceColumns(bindings string, parameters ...any) (string, []any) {
	if bindings == "" {
		return analysisSourceColumns, parameters
	}
	placeholder := "$" + strconv.Itoa(len(parameters)+1)
	revision := `('intro-source-v1-' || md5(jsonb_build_array(i.root_id,
		i.relative_path,i.file_identity,i.file_size,extract(epoch FROM i.modified_at),i.media,
		COALESCE((` + placeholder + `::jsonb->>i.root_id)::bigint,0))::text))`
	return analysisSourceColumnsPrefix + revision + analysisSourceColumnsSuffix, append(parameters, bindings)
}
