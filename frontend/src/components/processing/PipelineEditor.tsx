import { useState } from "react";
import type { ProcessingPipelineStep } from "../../lib/generated/contracts";
import { DEFAULT_PIPELINE } from "../../features/study/model";
import { workbenchActions } from "../../app/store/workbenchStore";

interface PipelineEditorProps {
  pipeline: readonly ProcessingPipelineStep[];
  busy: boolean;
}

export function PipelineEditor({ pipeline, busy }: PipelineEditorProps) {
  const [open, setOpen] = useState(false);

  function moveStep(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= pipeline.length) {
      return;
    }

    const next = [...pipeline];
    [next[index], next[target]] = [next[target], next[index]];
    workbenchActions.setProcessingPipeline(next);
  }

  return (
    <section className="form-section">
      <button
        className="form-collapse-toggle"
        type="button"
        onClick={() => setOpen((current) => !current)}
      >
        <span
          className={`form-collapse-arrow${open ? " form-collapse-arrow--open" : ""}`}
        >
          &#9654;
        </span>
        Advanced: Pipeline Order
      </button>

      {open && (
        <div className="pipeline-editor">
          <ul className="pipeline-list">
            {pipeline.map((step, index) => (
              <li key={step} className="pipeline-item">
                <span className="pipeline-item__name">{step}</span>
                <div className="pipeline-item__actions">
                  <button
                    className="pipeline-btn"
                    type="button"
                    onClick={() => moveStep(index, -1)}
                    disabled={index === 0 || busy}
                    aria-label={`Move ${step} up`}
                  >
                    &#9650;
                  </button>
                  <button
                    className="pipeline-btn"
                    type="button"
                    onClick={() => moveStep(index, 1)}
                    disabled={index === pipeline.length - 1 || busy}
                    aria-label={`Move ${step} down`}
                  >
                    &#9660;
                  </button>
                </div>
              </li>
            ))}
          </ul>
          <p className="form-hint">
            `grayscale` is always the starting point. Pseudocolor runs after
            the grayscale pipeline.
          </p>
          {pipeline.some((step, index) => step !== DEFAULT_PIPELINE[index]) && (
            <button
              className="button button--ghost pipeline-reset"
              type="button"
              onClick={() =>
                workbenchActions.setProcessingPipeline([...DEFAULT_PIPELINE])
              }
              disabled={busy}
            >
              Reset to default order
            </button>
          )}
        </div>
      )}
    </section>
  );
}
