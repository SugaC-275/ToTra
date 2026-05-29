import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { apiClient } from "../api/client";

interface FeedbackWidgetProps {
  logId: string;
  /** Called after successful submission with the submitted rating */
  onSubmitted?: (rating: 1 | -1) => void;
  /** Called when user clicks "Create eval case from this request" */
  onCreateEvalCase?: () => void;
}

interface FeedbackPayload {
  rating: 1 | -1;
  comment?: string;
}

async function submitFeedback(logId: string, payload: FeedbackPayload): Promise<void> {
  await apiClient.post(`/admin/logs/${logId}/feedback`, payload);
}

export function FeedbackWidget({ logId, onSubmitted, onCreateEvalCase }: FeedbackWidgetProps) {
  const [submitted, setSubmitted] = useState<1 | -1 | null>(null);
  const [showComment, setShowComment] = useState(false);
  const [comment, setComment] = useState("");
  const [pendingRating, setPendingRating] = useState<1 | -1 | null>(null);

  const mutation = useMutation({
    mutationFn: (payload: FeedbackPayload) => submitFeedback(logId, payload),
    onSuccess: (_, vars) => {
      setSubmitted(vars.rating);
      setShowComment(false);
      onSubmitted?.(vars.rating);
    },
  });

  function handleRatingClick(rating: 1 | -1) {
    if (submitted !== null) return;
    setPendingRating(rating);
    setShowComment(true);
  }

  function handleSubmit() {
    if (pendingRating === null) return;
    mutation.mutate({
      rating: pendingRating,
      comment: comment.trim() || undefined,
    });
  }

  function handleSkipComment() {
    if (pendingRating === null) return;
    mutation.mutate({ rating: pendingRating });
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-3">
        <span className="text-xs text-zinc-500">Was this response useful?</span>
        <button
          onClick={() => handleRatingClick(1)}
          disabled={submitted !== null || mutation.isPending}
          title="Thumbs up"
          className={[
            "text-base leading-none transition-opacity",
            submitted === null && !mutation.isPending ? "hover:opacity-100 opacity-70" : "",
            submitted === 1 ? "opacity-100" : "",
            submitted === -1 ? "opacity-30" : "",
            "disabled:cursor-not-allowed",
          ].join(" ")}
        >
          {submitted === 1 ? "👍" : "👍"}
        </button>
        <button
          onClick={() => handleRatingClick(-1)}
          disabled={submitted !== null || mutation.isPending}
          title="Thumbs down"
          className={[
            "text-base leading-none transition-opacity",
            submitted === null && !mutation.isPending ? "hover:opacity-100 opacity-70" : "",
            submitted === -1 ? "opacity-100" : "",
            submitted === 1 ? "opacity-30" : "",
            "disabled:cursor-not-allowed",
          ].join(" ")}
        >
          {submitted === -1 ? "👎" : "👎"}
        </button>
        {submitted !== null && (
          <span className="text-xs text-green-400">Feedback recorded</span>
        )}
        {mutation.isError && (
          <span className="text-xs text-red-400">Failed to submit</span>
        )}
      </div>

      {showComment && submitted === null && (
        <div className="space-y-2 pl-1">
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value.slice(0, 200))}
            placeholder="Optional comment (max 200 chars)…"
            rows={2}
            className="w-full resize-none rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-xs text-zinc-200 placeholder-zinc-600 focus:outline-none focus:border-indigo-500"
          />
          <div className="flex items-center gap-2">
            <span className="text-xs text-zinc-600">{comment.length}/200</span>
            <button
              onClick={handleSubmit}
              disabled={mutation.isPending}
              className="px-3 py-1 rounded bg-indigo-600 hover:bg-indigo-500 text-xs text-white disabled:opacity-50"
            >
              {mutation.isPending ? "Submitting…" : "Submit"}
            </button>
            <button
              onClick={handleSkipComment}
              disabled={mutation.isPending}
              className="px-3 py-1 rounded bg-zinc-800 hover:bg-zinc-700 text-xs text-zinc-400 disabled:opacity-50"
            >
              Skip
            </button>
          </div>
        </div>
      )}

      {submitted !== null && onCreateEvalCase && (
        <div className="pl-1">
          <button
            onClick={onCreateEvalCase}
            className="text-xs text-indigo-400 hover:underline"
          >
            + Create eval case from this request
          </button>
        </div>
      )}
    </div>
  );
}
