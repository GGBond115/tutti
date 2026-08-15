import { Button } from "@tutti-os/ui-system";

export function AgentGUITuttiPlanReviewAction({
  label,
  onRequestChanges
}: {
  label: string;
  onRequestChanges(): void;
}): React.JSX.Element {
  return (
    <Button
      type="button"
      variant="secondary"
      size="sm"
      className="rounded-full"
      data-testid="agent-gui-plan-request-changes"
      onClick={onRequestChanges}
    >
      {label}
    </Button>
  );
}
