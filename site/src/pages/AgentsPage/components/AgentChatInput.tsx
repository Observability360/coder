import { isAxiosError } from "axios";
import {
	ArrowLeftIcon,
	ArrowUpIcon,
	CheckIcon,
	ChevronRightIcon,
	MessageCircleQuestionIcon,
	MicIcon,
	MonitorIcon,
	PaperclipIcon,
	PencilIcon,
	PlusIcon,
	ServerIcon,
	SquareIcon,
	UnlinkIcon,
	XIcon,
} from "lucide-react";
import type React from "react";
import {
	type FC,
	useEffect,
	useImperativeHandle,
	useRef,
	useState,
} from "react";
import { useMutation, useQuery, useQueryClient } from "react-query";
import { Link } from "react-router";
import { toast } from "sonner";
import { API } from "#/api/api";
import type { Repository } from "#/api/api";
import { getErrorMessage } from "#/api/errors";
import { disconnectMCPServerOAuth2 } from "#/api/queries/chats";
import {
	attachRepositoryWorkspace,
	repositoryCatalog,
} from "#/api/queries/repositoryCatalog";
import type * as TypesGen from "#/api/typesGenerated";
import type {
	AgentChatSendShortcut,
	ChatQueuedMessage,
} from "#/api/typesGenerated";
import { Alert, AlertDescription } from "#/components/Alert/Alert";
import { Button } from "#/components/Button/Button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "#/components/Command/Command";
import { ConfirmDialog } from "#/components/Dialog/ConfirmDialog/ConfirmDialog";
import { ExternalImage } from "#/components/ExternalImage/ExternalImage";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "#/components/Popover/Popover";
import { Separator } from "#/components/Separator/Separator";
import { Skeleton } from "#/components/Skeleton/Skeleton";
import { Spinner } from "#/components/Spinner/Spinner";
import { Switch } from "#/components/Switch/Switch";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "#/components/Tooltip/Tooltip";
import { cn } from "#/utils/cn";
import { countInvisibleCharacters } from "#/utils/invisibleUnicode";
import { isBelowMdViewport, isMobileViewport } from "#/utils/mobile";
import { useAudioTranscription } from "../hooks/useAudioTranscription";
import { estimateContextUsage } from "../utils/contextEstimate";
import { chatWidthClass, useChatFullWidth } from "../hooks/useChatFullWidth";
import { useMCPOAuthFlow } from "../hooks/useMCPOAuthFlow";
import { useOverflowCount } from "../hooks/useOverflowCount";
import {
	DEFAULT_AGENT_CHAT_SEND_SHORTCUT,
	MODIFIER_AGENT_CHAT_SEND_SHORTCUT,
} from "../utils/agentChatSendShortcut";
import {
	chatAttachmentAcceptAttribute,
	isChatAttachmentFile,
} from "../utils/chatAttachments";
import type { ChatSlashCommand } from "../utils/slashCommands";
import { AgentSetupNotice } from "./AgentSetupNotice";
import {
	AttachmentPreview,
	isUploadInProgress,
	type UploadState,
} from "./AttachmentPreview";
import { ModelSelector, type ModelSelectorOption } from "./ChatElements";
import {
	ChatMessageInput,
	type ChatMessageInputRef,
} from "./ChatMessageInput/ChatMessageInput";
import type { SkillMetadata } from "./ChatMessageInput/SkillsTriggerMenu";
import type { AgentContextUsage } from "./ContextUsageIndicator";
import { ContextUsageIndicator } from "./ContextUsageIndicator";
import { ImageLightbox } from "./ImageLightbox";
import { QueuedMessagesList } from "./QueuedMessagesList";
import { TextPreviewDialog } from "./TextPreviewDialog";
import { WorkspacePill } from "./WorkspacePill";

export {
	ImageThumbnail,
	isUploadInProgress,
	type UploadState,
} from "./AttachmentPreview";
export type { ChatMessageInputRef } from "./ChatMessageInput/ChatMessageInput";
export type { AgentContextUsage } from "./ContextUsageIndicator";

interface AgentChatInputProps {
	onSend: (message: string) => void;
	sendShortcut?: AgentChatSendShortcut;
	placeholder?: string;
	isDisabled: boolean;
	isLoading: boolean;
	// Ref for the Lexical editor, exposed for imperative access.
	inputRef?: React.Ref<ChatMessageInputRef>;
	// Initial text to seed the editor on first mount only.
	initialValue?: string;
	// Serialized Lexical editor state for restoring drafts with
	// file-reference chips. Takes precedence over initialValue.
	initialEditorState?: string;
	// Monotonic counter to force editor remount.
	remountKey?: number;
	// Called on every content change inside the editor.
	onContentChange?: (
		content: string,
		serializedEditorState: string,
		hasFileReferences: boolean,
	) => void;
	// Model selector.
	selectedModel: string;
	onModelChange: (value: string) => void;
	modelOptions: readonly ModelSelectorOption[];
	modelSelectorPlaceholder: string;
	hasModelOptions: boolean;
	reasoningEffort?: string;
	onReasoningEffortChange?: (value: string) => void;
	planModeEnabled?: boolean;
	onPlanModeToggle?: (enabled: boolean) => void;
	isModelCatalogLoading?: boolean;
	// Streaming controls (optional, for the detail page).
	isStreaming?: boolean;
	onInterrupt?: () => void;
	isInterruptPending?: boolean;
	// Workspace picker.
	workspaceOptions?: ReadonlyArray<{
		id: string;
		name: string;
		owner_name: string;
		organization_id: string;
	}>;
	selectedWorkspaceId?: string | null;
	onWorkspaceChange?: (id: string | null) => void;
	// Organization ID of the current chat. When set, workspaces from
	// other organizations are shown as disabled in the picker.
	chatOrganizationId?: string;
	isWorkspaceLoading?: boolean;
	// Queued user messages rendered above the textarea.
	queuedMessages?: readonly ChatQueuedMessage[];
	onDeleteQueuedMessage?: (id: number) => Promise<void> | void;
	onPromoteQueuedMessage?: (id: number) => Promise<void> | void;
	// History editing state, owned by the parent.
	isEditingHistoryMessage?: boolean;
	onCancelHistoryEdit?: () => void;
	// Newest-first list of non-empty user prompts for local history cycling.
	userPromptHistory?: readonly string[];

	// Optional context-usage summary shown to the left of the send button.
	// Pass `null` to render fallback values (e.g. when limit is unknown).
	// Omit entirely to hide the indicator.
	contextUsage?: AgentContextUsage | null;
	// Re-pins the chat to the workspace's latest context snapshot,
	// surfaced by the context indicator when the pinned context has
	// drifted.
	onRefreshContext?: () => void;
	isRefreshingContext?: boolean;
	attachments?: readonly File[];
	onAttach?: (files: File[]) => void;
	onRemoveAttachment?: (attachment: number | File) => void;
	uploadStates?: Map<File, UploadState>;
	previewUrls?: Map<File, string>;
	textContents?: Map<File, string>;
	onTextPreview?: (
		content: string,
		fileName: string,
		mediaType?: string,
	) => void;
	// MCP Server picker.
	mcpServers?: readonly TypesGen.MCPServerConfig[];
	selectedMCPServerIds?: readonly string[];
	onMCPSelectionChange?: (ids: string[]) => void;
	onMCPAuthComplete?: (serverId: string) => void;
	workspaceSkills?: readonly SkillMetadata[];
	workspace?: TypesGen.Workspace;
	workspaceAgent?: TypesGen.WorkspaceAgent;
	chatId?: string;
	sshCommand?: string;
	attachedWorkspace?: AttachedWorkspaceInfo;
	folder?: string;
	canConfigureAgentSetup: boolean;
	providerCount?: number;
	modelCount?: number;
	unsupportedProviderNames?: readonly string[];
	// AI Gateway is disabled deployment-wide, independent of provider/model
	// configuration. Forces the setup notice regardless of the counts above.
	aiGatewayDisabled?: boolean;
	// Built-in commands offered by the "/" trigger menu ahead of
	// personal skills.
	slashCommands?: readonly ChatSlashCommand[];
}

export interface AttachedWorkspaceInfo {
	id: string;
	name: string;
	route: string;
	statusIcon: React.ReactNode;
	statusLabel: string;
}
// Shared pill sizing: flex-basis sets a ~8ch floor (shrink-0 enforces
// it), grow expands into free row space, and max-w-max caps at the
// label's natural width. Below the floor the +N overflow takes over.
const pillSizingClasses =
	"grow shrink-0 basis-[calc(8ch_+_3.125rem)] max-w-max";

type ToolBadgeData =
	| { kind: "workspace"; name: string }
	| ({ kind: "attached-workspace" } & AttachedWorkspaceInfo)
	| { kind: "mcp"; server: TypesGen.MCPServerConfig }
	| { kind: "planning" };

// Small `X` button rendered inside pill-style badges (attached
// workspace, MCP server, planning indicator) to dismiss or disable
// the badge without opening the `+` menu. Callers pass the action
// handler and a descriptive aria-label.
const BadgeDismissButton: FC<{
	onClick: () => void;
	ariaLabel: string;
	isDisabled?: boolean;
}> = ({ onClick, ariaLabel, isDisabled = false }) => (
	<button
		type="button"
		onClick={onClick}
		disabled={isDisabled}
		className="group -mx-1 -my-1 inline-flex size-5 cursor-pointer items-center justify-center rounded-full border-0 bg-transparent p-0 text-content-secondary disabled:cursor-not-allowed disabled:opacity-50"
		aria-label={ariaLabel}
	>
		<span className="inline-flex size-3.5 items-center justify-center rounded-full transition-colors group-hover:bg-surface-tertiary group-hover:text-content-primary">
			<XIcon className="!size-2.5" />
		</span>
	</button>
);

const ToolBadge: FC<{
	badge: ToolBadgeData;
	onRemoveWorkspace?: () => void;
	onRemoveMcp?: (serverId: string) => void;
	onRemovePlanning?: () => void;
	isDisabled?: boolean;
	className?: string;
	// The overflow popover auto-focuses badges; suppress the tooltip there.
	disableTooltip?: boolean;
}> = ({
	badge,
	onRemoveWorkspace,
	onRemoveMcp,
	onRemovePlanning,
	isDisabled,
	className,
	disableTooltip,
}) => {
	const badgeCls = cn(
		"inline-flex shrink-0 items-center gap-1 rounded-full bg-surface-secondary px-2 py-0.5 text-xs font-medium text-content-secondary",
		className,
	);

	if (badge.kind === "planning") {
		return (
			<span data-testid="planning-badge" className={badgeCls}>
				<PencilIcon className="size-3" />
				Planning
				{onRemovePlanning && (
					<BadgeDismissButton
						onClick={onRemovePlanning}
						ariaLabel="Disable plan mode"
						isDisabled={isDisabled}
					/>
				)}
			</span>
		);
	}

	if (badge.kind === "attached-workspace") {
		return (
			<Tooltip>
				<TooltipTrigger asChild>
					<span
						className={cn(
							badgeCls,
							"transition-colors hover:bg-surface-tertiary hover:text-content-primary",
						)}
					>
						<Link
							to={badge.route}
							target="_blank"
							rel="noreferrer"
							className="inline-flex min-w-0 items-center gap-1 text-inherit no-underline"
						>
							{badge.statusIcon}
							<span className="truncate">{badge.name}</span>
						</Link>
						{onRemoveWorkspace && (
							<BadgeDismissButton
								onClick={onRemoveWorkspace}
								ariaLabel={`Remove workspace ${badge.name}`}
							/>
						)}
					</span>
				</TooltipTrigger>
				{/* Hidden below md: touch focus would stick the tooltip open. */}
				{!disableTooltip && (
					<TooltipContent className="hidden md:block">
						{badge.statusLabel}
					</TooltipContent>
				)}
			</Tooltip>
		);
	}

	if (badge.kind === "workspace") {
		return (
			<span className={badgeCls}>
				<MonitorIcon className="size-3" />
				<span className="truncate">{badge.name}</span>
				{onRemoveWorkspace && (
					<BadgeDismissButton
						onClick={onRemoveWorkspace}
						ariaLabel={`Remove workspace ${badge.name}`}
					/>
				)}
			</span>
		);
	}

	const isForceOn = badge.server.availability === "force_on";
	return (
		<span className={badgeCls}>
			{badge.server.icon_url ? (
				<ExternalImage
					src={badge.server.icon_url}
					alt=""
					className="size-3 rounded-sm"
				/>
			) : (
				<ServerIcon className="size-3" />
			)}
			{badge.server.display_name}
			{!isForceOn && onRemoveMcp && (
				<BadgeDismissButton
					onClick={() => onRemoveMcp(badge.server.id)}
					ariaLabel={`Remove ${badge.server.display_name}`}
				/>
			)}
		</span>
	);
};

export const AgentChatInput: FC<AgentChatInputProps> = ({
	onSend,
	sendShortcut = DEFAULT_AGENT_CHAT_SEND_SHORTCUT,
	placeholder = "Type a message...",
	isDisabled,
	isLoading,
	inputRef,
	initialValue,
	initialEditorState,
	remountKey,
	onContentChange,
	selectedModel,
	onModelChange,
	modelOptions,
	modelSelectorPlaceholder,
	hasModelOptions,
	reasoningEffort,
	onReasoningEffortChange,
	planModeEnabled = false,
	onPlanModeToggle,
	isModelCatalogLoading = false,
	isStreaming = false,
	onInterrupt,
	isInterruptPending = false,
	workspaceOptions,
	selectedWorkspaceId,
	onWorkspaceChange,
	chatOrganizationId,
	isWorkspaceLoading,
	queuedMessages = [],
	onDeleteQueuedMessage,
	onPromoteQueuedMessage,
	isEditingHistoryMessage = false,
	onCancelHistoryEdit,
	userPromptHistory = [],
	contextUsage,
	onRefreshContext,
	isRefreshingContext,
	attachments = [],
	onAttach,
	onRemoveAttachment,
	uploadStates,
	previewUrls,
	textContents,
	onTextPreview,
	mcpServers,
	selectedMCPServerIds,
	onMCPSelectionChange,
	onMCPAuthComplete,
	workspaceSkills,
	workspace,
	workspaceAgent,
	chatId,
	sshCommand,
	attachedWorkspace,
	folder,
	canConfigureAgentSetup,
	providerCount,
	modelCount,
	unsupportedProviderNames = [],
	aiGatewayDisabled,
	slashCommands,
}) => {
	const [chatFullWidth] = useChatFullWidth();
	const showAgentSetupNotice =
		aiGatewayDisabled ||
		(canConfigureAgentSetup
			? providerCount !== undefined &&
				modelCount !== undefined &&
				(providerCount === 0 || modelCount === 0)
			: modelCount !== undefined && modelCount === 0);
	const internalRef = useRef<ChatMessageInputRef>(null);

	// Toolbar entry point for the "/" skills menu: inserting the slash
	// through the editor lets SkillsTriggerPlugin open the same menu the
	// typed trigger uses, so search/keyboard/selection behave identically.
	const handleSkillsMenuButton = () => {
		const input = internalRef.current;
		if (!input) return;
		input.focus();
		const value = input.getValue();
		if (value.endsWith("/")) return;
		const needsSpace = value.length > 0 && !/\s$/.test(value);
		input.insertText(needsSpace ? " /" : "/");
	};
	const [previewImage, setPreviewImage] = useState<string | null>(null);
	const [previewText, setPreviewText] = useState<string | null>(null);
	const [previewTextFileName, setPreviewTextFileName] = useState<string | null>(
		null,
	);
	const [previewTextMediaType, setPreviewTextMediaType] = useState<
		string | null
	>(null);
	const [plusMenuOpen, setPlusMenuOpen] = useState(false);
	const [plusMenuView, setPlusMenuView] = useState<"main" | "workspace">(
		"main",
	);
	const [workspacePickerOpen, setWorkspacePickerOpen] = useState(false);
	const { connectingServerId: mcpConnectingId, connect: connectMCPServer } =
		useMCPOAuthFlow({
			organizationId: chatOrganizationId,
			onAuthComplete: onMCPAuthComplete,
			onFlowSuccess: (serverID) => {
				if (
					onMCPSelectionChange &&
					selectedMCPServerIds &&
					mcpServers?.some(
						(server) => server.id === serverID && server.enabled,
					) &&
					!selectedMCPServerIds.includes(serverID)
				) {
					onMCPSelectionChange([...selectedMCPServerIds, serverID]);
				}
			},
		});
	const [mcpDisconnectTarget, setMcpDisconnectTarget] =
		useState<TypesGen.MCPServerConfig | null>(null);
	const queryClient = useQueryClient();
	const mcpDisconnectMutation = useMutation(
		disconnectMCPServerOAuth2(queryClient),
	);

	const [hasFileReferences, setHasFileReferences] = useState(false);
	const [cycleIndex, setCycleIndex] = useState<number | null>(null);
	const [cycleSavedDraft, setCycleSavedDraft] = useState<string | null>(null);
	const cycleHistorySnapshotRef = useRef<readonly string[] | null>(null);
	const currentCycleValueRef = useRef<string | null>(null);
	const previousRemountKeyRef = useRef(remountKey);

	const resetPromptCycle = () => {
		setCycleIndex(null);
		setCycleSavedDraft(null);
		cycleHistorySnapshotRef.current = null;
		currentCycleValueRef.current = null;
	};

	const applyCycleValue = (text: string) => {
		const editor = internalRef.current;
		if (!editor) return;
		currentCycleValueRef.current = text;
		editor.setValue(text);
		editor.focus();
	};

	useEffect(() => {
		if (previousRemountKeyRef.current === remountKey) return;
		previousRemountKeyRef.current = remountKey;
		// Inlined resetPromptCycle body. Calling resetPromptCycle directly
		// would force it into the dep array; the React Compiler stabilises
		// callbacks but biome's react-hooks lint does not.
		setCycleIndex(null);
		setCycleSavedDraft(null);
		cycleHistorySnapshotRef.current = null;
		currentCycleValueRef.current = null;
		// Keep in sync with resetPromptCycle above.
	}, [remountKey]);

	const speech = useAudioTranscription();
	const [preRecordingValue, setPreRecordingValue] = useState<string>("");
	// "BTW" (ask without interrupting): a read-only side query answered
	// from this local state only -- never written into chatStore or the
	// chat's own transcript, and cleared on dismiss or on sending a real
	// message.
	const [btwResult, setBtwResult] =
		useState<TypesGen.ChatSideQueryResponse | null>(null);
	const [btwPending, setBtwPending] = useState(false);
	const [btwError, setBtwError] = useState<string | null>(null);
	// Live draft length for the pre-send context estimate below -- mirrors
	// the existing invisibleCharCount pattern (derived from the same
	// onChange content string, not a new editor subscription).
	const [draftCharCount, setDraftCharCount] = useState(0);
	const selectedModelOption = modelOptions.find(
		(option) => option.id === selectedModel,
	);
	// Prefer effectiveContextLimit (the largest window any currently
	// eligible target in a combo model can serve) over the raw
	// contextLimit (the smallest target's window) so the pre-send
	// estimate doesn't warn/overflow just because the combo's primary
	// target is too small when a bigger target remains eligible.
	// effectiveContextLimit is undefined for every model until the
	// backend populates it, so this is a no-op fallback to today's
	// behavior until then.
	const selectedModelContextLimit =
		selectedModelOption?.effectiveContextLimit ??
		selectedModelOption?.contextLimit;
	const contextEstimate = estimateContextUsage({
		realUsage: contextUsage ?? null,
		limitTokens: selectedModelContextLimit,
		compressionThresholdPercent: contextUsage?.compressionThreshold,
		draftCharCount,
		queuedMessages,
	});

	// Unlike the old Web Speech-based hook (live word-by-word interim
	// results, fired only WHILE isRecording), transcript here changes
	// exactly ONCE per recording — after stop() triggers the
	// record→upload→transcribe round trip and isRecording has already
	// flipped back to false. Gating on transcript itself (not isRecording)
	// is what makes the one-shot insertion fire correctly.
	useEffect(() => {
		if (!speech.transcript) return;
		const editor = internalRef.current;
		if (!editor) return;
		editor.clear();
		const combined = preRecordingValue
			? `${preRecordingValue} ${speech.transcript}`
			: speech.transcript;
		editor.insertText(combined);
	}, [speech.transcript, preRecordingValue]);

	// Forward a stable delegating handle to the parent-supplied inputRef.
	// Delegates lazily to internalRef.current so methods see the current
	// Lexical instance after a remount, not the orphaned ref captured at
	// factory time.
	useImperativeHandle(
		inputRef,
		() => ({
			setValue: (text) => internalRef.current?.setValue(text),
			insertText: (text) => internalRef.current?.insertText(text),
			clear: () => internalRef.current?.clear(),
			focus: () => internalRef.current?.focus(),
			getValue: () => internalRef.current?.getValue() ?? "",
			addFileReference: (ref) => internalRef.current?.addFileReference(ref),
			getContentParts: () => internalRef.current?.getContentParts() ?? [],
		}),
		[],
	);

	const handleMcpToggle = (serverId: string, checked: boolean) => {
		if (!onMCPSelectionChange || !selectedMCPServerIds) return;
		if (checked) {
			onMCPSelectionChange([...selectedMCPServerIds, serverId]);
		} else {
			onMCPSelectionChange(
				selectedMCPServerIds.filter((id) => id !== serverId),
			);
		}
	};

	const handleMcpDisconnectConfirm = () => {
		if (!mcpDisconnectTarget) {
			return;
		}
		const name = mcpDisconnectTarget.display_name;
		mcpDisconnectMutation.mutate(mcpDisconnectTarget.id, {
			onSuccess: (response) => {
				setMcpDisconnectTarget(null);
				if (response.token_revocation_error) {
					toast.warning(`Disconnected ${name}.`, {
						description: response.token_revocation_error,
					});
				} else {
					toast.success(`Disconnected ${name}.`);
				}
			},
			onError: (error) => {
				toast.error(getErrorMessage(error, `Failed to disconnect ${name}.`));
			},
		});
	};

	// Only a chat-bound workspace counts: an unbound selection (new chat
	// form, or a picked workspace before the first send) has no pinned
	// context to resolve, so treating it as a workspace would leave the
	// menu in the loading state forever.
	const hasSkillsWorkspace = Boolean(attachedWorkspace?.id ?? workspace?.id);

	const selectedWorkspace = workspaceOptions?.find(
		(ws) => ws.id === selectedWorkspaceId,
	);
	const canUseWorkspacePicker =
		Boolean(onWorkspaceChange) && !isWorkspaceLoading;
	const linkedWorkspaceId = workspace?.id ?? attachedWorkspace?.id;

	const shouldShowSelectedWorkspaceBadge = selectedWorkspace
		? selectedWorkspace.id !== linkedWorkspaceId
		: false;

	const enabledMcpServers = mcpServers?.filter((s) => s.enabled) ?? [];
	const activeMcpServers = enabledMcpServers.filter(
		(s) =>
			(s.availability === "force_on" || selectedMCPServerIds?.includes(s.id)) &&
			!(s.auth_type === "oauth2" && !s.auth_connected),
	);

	const badgeContainerRef = useRef<HTMLDivElement>(null);

	const [overflowPopoverOpen, setOverflowPopoverOpen] = useState(false);
	const shouldOverflowPlanningBadge =
		planModeEnabled && contextUsage !== undefined;

	let workspacePillBadge: ToolBadgeData | undefined;
	if (workspace && workspaceAgent && chatId) {
		workspacePillBadge = attachedWorkspace
			? { kind: "attached-workspace", ...attachedWorkspace }
			: { kind: "workspace", name: workspace.name };
	}

	// Ordered list of active tool badge data so we can determine
	// which ones ended up in the overflow popover.
	const allBadges: ToolBadgeData[] = [];
	if (shouldOverflowPlanningBadge) {
		allBadges.push({ kind: "planning" });
	}
	if (workspacePillBadge) {
		allBadges.push(workspacePillBadge);
	} else if (attachedWorkspace) {
		allBadges.push({ kind: "attached-workspace", ...attachedWorkspace });
	}
	if (shouldShowSelectedWorkspaceBadge && selectedWorkspace) {
		allBadges.push({ kind: "workspace", name: selectedWorkspace.name });
	}
	for (const s of activeMcpServers) {
		allBadges.push({ kind: "mcp", server: s });
	}

	const overflowCount = useOverflowCount(badgeContainerRef, allBadges.length);
	const visibleCount = Math.max(0, allBadges.length - overflowCount);
	const overflowBadges = allBadges.slice(visibleCount);

	const handleRemoveWorkspace = () => onWorkspaceChange?.(null);
	const removeWorkspaceHandler = onWorkspaceChange
		? handleRemoveWorkspace
		: undefined;
	const handleRemoveMcp = (serverId: string) =>
		handleMcpToggle(serverId, false);

	const handlePlanModeToggle = () => {
		onPlanModeToggle?.(!planModeEnabled);
		setPlusMenuOpen(false);
	};

	const handleDisablePlanMode = () => onPlanModeToggle?.(false);

	const fileInputRef = useRef<HTMLInputElement>(null);
	const [composerElement, setComposerElement] = useState<HTMLDivElement | null>(
		null,
	);
	useEffect(() => {
		if (!composerElement) return;
		// Radix popover wrappers are fixed-positioned, so their
		// inset values need to be in layout-viewport coordinates.
		// The visual viewport can be offset inside the layout
		// viewport when the mobile keyboard is open. Treat
		// `visualViewport.offsetTop` as a clamp only when it yields
		// a positive height, since mobile WebKit can report mixed
		// coordinate systems while the keyboard is settling.
		const viewport = globalThis.visualViewport;
		const root = document.documentElement;
		const fixedProbe = document.createElement("div");
		Object.assign(fixedProbe.style, {
			position: "fixed",
			bottom: "0",
			left: "0",
			width: "0",
			height: "0",
			pointerEvents: "none",
			visibility: "hidden",
		});
		document.body.appendChild(fixedProbe);
		const composerGap = 8;
		const viewportPadding = 16;
		const minimumMenuHeight = 96;
		const update = () => {
			const rect = composerElement.getBoundingClientRect();
			const fixedViewportBottom = fixedProbe.getBoundingClientRect().bottom;
			const visibleViewportTop = viewport?.offsetTop ?? 0;
			const bottom = Math.max(0, fixedViewportBottom - rect.bottom);
			// Keep the dropdown's bottom edge above the software keyboard,
			// which covers the bottom of the layout viewport without moving
			// fixed-positioned elements.
			const keyboardInset = viewport
				? Math.max(
						0,
						fixedViewportBottom - (viewport.offsetTop + viewport.height),
					)
				: 0;
			const aboveComposerBottom = Math.max(
				0,
				fixedViewportBottom - rect.top + composerGap,
				keyboardInset + composerGap,
			);
			const dropdownBottomEdgeTop = fixedViewportBottom - aboveComposerBottom;
			const maxHeightCandidates = [
				dropdownBottomEdgeTop - visibleViewportTop - viewportPadding,
				dropdownBottomEdgeTop - viewportPadding,
			].filter((height) => height > 0);
			const aboveComposerMaxHeight = Math.max(
				minimumMenuHeight,
				maxHeightCandidates.length > 0 ? Math.min(...maxHeightCandidates) : 0,
			);
			root.style.setProperty("--mobile-dropdown-bottom", `${bottom}px`);
			root.style.setProperty("--mobile-dropdown-left", `${rect.left}px`);
			root.style.setProperty("--mobile-dropdown-width", `${rect.width}px`);
			root.style.setProperty(
				"--mobile-dropdown-above-composer-bottom",
				`${aboveComposerBottom}px`,
			);
			root.style.setProperty(
				"--mobile-dropdown-above-composer-max-height",
				`${aboveComposerMaxHeight}px`,
			);
		};
		const animationFrameIDs = new Set<number>();
		const timeoutIDs = new Set<ReturnType<typeof setTimeout>>();
		const cancelScheduledUpdates = () => {
			for (const id of animationFrameIDs) {
				cancelAnimationFrame(id);
			}
			animationFrameIDs.clear();
			for (const id of timeoutIDs) {
				clearTimeout(id);
			}
			timeoutIDs.clear();
		};
		const queueAnimationFrame = (callback: () => void) => {
			const id = requestAnimationFrame(() => {
				animationFrameIDs.delete(id);
				callback();
			});
			animationFrameIDs.add(id);
		};
		const scheduleUpdate = () => {
			cancelScheduledUpdates();
			update();
			// Mobile WebKit can finish keyboard panning after focus and
			// input events. Re-read geometry after the viewport settles so
			// the first slash-menu render is not stuck under the composer.
			queueAnimationFrame(() => {
				update();
				queueAnimationFrame(update);
			});
			for (const delay of [50, 150, 300]) {
				const id = setTimeout(() => {
					timeoutIDs.delete(id);
					update();
				}, delay);
				timeoutIDs.add(id);
			}
		};
		scheduleUpdate();
		const ro = new ResizeObserver(scheduleUpdate);
		ro.observe(composerElement);
		addEventListener("resize", scheduleUpdate);
		addEventListener("scroll", scheduleUpdate, { passive: true });
		addEventListener("focusin", scheduleUpdate);
		addEventListener("focusout", scheduleUpdate);
		composerElement.addEventListener("input", scheduleUpdate);
		composerElement.addEventListener("keyup", scheduleUpdate);
		document.addEventListener("selectionchange", scheduleUpdate);
		viewport?.addEventListener("resize", scheduleUpdate);
		viewport?.addEventListener("scroll", scheduleUpdate);
		viewport?.addEventListener("scrollend", scheduleUpdate);
		return () => {
			ro.disconnect();
			cancelScheduledUpdates();
			removeEventListener("resize", scheduleUpdate);
			removeEventListener("scroll", scheduleUpdate);
			removeEventListener("focusin", scheduleUpdate);
			removeEventListener("focusout", scheduleUpdate);
			composerElement.removeEventListener("input", scheduleUpdate);
			composerElement.removeEventListener("keyup", scheduleUpdate);
			document.removeEventListener("selectionchange", scheduleUpdate);
			viewport?.removeEventListener("resize", scheduleUpdate);
			viewport?.removeEventListener("scroll", scheduleUpdate);
			viewport?.removeEventListener("scrollend", scheduleUpdate);
			fixedProbe.remove();
			root.style.removeProperty("--mobile-dropdown-bottom");
			root.style.removeProperty("--mobile-dropdown-left");
			root.style.removeProperty("--mobile-dropdown-width");
			root.style.removeProperty("--mobile-dropdown-above-composer-bottom");
			root.style.removeProperty("--mobile-dropdown-above-composer-max-height");
		};
	}, [composerElement]);

	const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
		if (e.target.files && onAttach) {
			resetPromptCycle();
			onAttach(Array.from(e.target.files));
		}
		// Reset so the same file can be selected again.
		e.target.value = "";
	};

	const handleFilePaste = (file: File) => {
		resetPromptCycle();
		onAttach?.([file]);
	};

	const handleInlineText = (file: File, nextContent?: string) => {
		const content = nextContent ?? textContents?.get(file);
		if (content === undefined) return;
		const editor = internalRef.current;
		if (!editor) return;
		resetPromptCycle();
		editor.insertText(content);
		onRemoveAttachment?.(file);
	};

	const handleTextPreview = (
		content: string,
		fileName: string,
		mediaType?: string,
	) => {
		if (onTextPreview) {
			onTextPreview(content, fileName, mediaType);
		} else {
			setPreviewText(content);
			setPreviewTextFileName(fileName);
			setPreviewTextMediaType(mediaType ?? null);
		}
	};

	// Drag-and-drop support for any chat-supported file type.
	const [isDragging, setIsDragging] = useState(false);

	const handleDragOver = (e: React.DragEvent) => {
		e.preventDefault();
		if (e.dataTransfer.types.includes("Files")) {
			setIsDragging(true);
		}
	};

	const handleDragLeave = (e: React.DragEvent) => {
		if (!e.currentTarget.contains(e.relatedTarget as Node)) {
			setIsDragging(false);
		}
	};

	const handleDrop = (e: React.DragEvent) => {
		e.preventDefault();
		setIsDragging(false);
		if (!onAttach || !e.dataTransfer.files.length) return;
		const attachable = Array.from(e.dataTransfer.files).filter(
			isChatAttachmentFile,
		);
		if (attachable.length === 0) return;
		resetPromptCycle();
		onAttach(attachable);
	};

	// Track whether the editor has content so we can gate the
	// send button without a controlled value prop.
	const [hasContent, setHasContent] = useState(() =>
		Boolean(initialValue?.trim()),
	);

	const [invisibleCharCount, setInvisibleCharCount] = useState(() =>
		countInvisibleCharacters(initialValue ?? ""),
	);

	const handleContentChange = (
		content: string,
		serializedEditorState: string,
		hasRefs: boolean,
	) => {
		// Lexical fires onChange synchronously from editor.setValue().
		// While cycling, compare incoming content to currentCycleValueRef,
		// the last value we applied. Different content means user input,
		// so reset; matching content is our own setValue echo, so keep cycling.
		// This works because React batches state updates within event handlers
		// and commits them after the handler returns, so the synchronous onChange
		// callback sees the pre-batch cycleIndex value, not the queued update.
		if (cycleIndex !== null && content !== currentCycleValueRef.current) {
			resetPromptCycle();
		}
		setHasContent(Boolean(content.trim()));
		setHasFileReferences(hasRefs);
		setInvisibleCharCount(countInvisibleCharacters(content));
		setDraftCharCount(content.length);
		onContentChange?.(content, serializedEditorState, hasRefs);
	};

	// Re-focus the editor after a send completes (isLoading goes
	// from true → false) so the user can immediately type again.
	const prevIsLoadingRef = useRef(isLoading);
	useEffect(() => {
		const wasLoading = prevIsLoadingRef.current;
		prevIsLoadingRef.current = isLoading;
		if (wasLoading && !isLoading && !isMobileViewport()) {
			internalRef.current?.focus();
		}
	}, [isLoading]);
	const hasActiveUploads = attachments.some((file) =>
		isUploadInProgress(uploadStates?.get(file)),
	);
	const hasUploadedAttachments = attachments.some(
		(f) => uploadStates?.get(f)?.status === "uploaded",
	);
	const hasDraftContext =
		hasContent || attachments.length > 0 || hasFileReferences;
	const isComposerEffectivelyEmpty = !hasDraftContext;
	const hasSendableContent =
		hasContent || hasUploadedAttachments || hasFileReferences;
	const canSend =
		!isDisabled &&
		!isLoading &&
		hasModelOptions &&
		hasSendableContent &&
		!hasActiveUploads;
	const BTW_PREFIX_RE = /^\/btw\b\s*(.*)$/is;
	const handleSideQuery = (question: string) => {
		if (!chatId) {
			return;
		}
		setBtwPending(true);
		setBtwError(null);
		void API.experimental
			.sideQuery(chatId, { question })
			.then((result) => {
				setBtwResult(result);
			})
			.catch((err: unknown) => {
				setBtwError(getErrorMessage(err, "Failed to get chat status."));
			})
			.finally(() => {
				setBtwPending(false);
			});
	};
	const handleDismissBtw = () => {
		setBtwResult(null);
		setBtwError(null);
	};
	const handleSubmit = () => {
		const text = internalRef.current?.getValue()?.trim() ?? "";

		const btwMatch = BTW_PREFIX_RE.exec(text);
		if (btwMatch && chatId) {
			handleSideQuery(btwMatch[1] ?? "");
			internalRef.current?.clear();
			resetPromptCycle();
			return;
		}

		// If the input is empty and there are queued messages,
		// promote the first one instead of submitting.
		if (
			!text &&
			!hasUploadedAttachments &&
			!hasFileReferences &&
			!isDisabled &&
			!isLoading &&
			!hasActiveUploads &&
			queuedMessages.length > 0 &&
			onPromoteQueuedMessage
		) {
			void onPromoteQueuedMessage(queuedMessages[0].id);
			return;
		}

		if (
			(!text && !hasUploadedAttachments && !hasFileReferences) ||
			isDisabled ||
			isLoading ||
			hasActiveUploads ||
			!hasModelOptions
		) {
			return;
		}

		onSend(text);
		resetPromptCycle();
		if (!isMobileViewport()) {
			internalRef.current?.focus();
		}
	};
	const handleStartRecording = () => {
		resetPromptCycle();
		setPreRecordingValue(internalRef.current?.getValue()?.trim() ?? "");
		speech.start();
	};

	const handleAcceptRecording = () => {
		speech.stop();
	};

	const handleCancelRecording = () => {
		const original = preRecordingValue;
		speech.cancel();
		const editor = internalRef.current;
		if (editor) {
			editor.clear();
			if (original) {
				editor.insertText(original);
			}
		}
		setPreRecordingValue("");
	};

	const handleComposerKeyDown = (e: React.KeyboardEvent) => {
		if (e.key === "Escape") {
			if (isEditingHistoryMessage) {
				e.preventDefault();
				onCancelHistoryEdit?.();
			} else if (isStreaming && onInterrupt && !isInterruptPending) {
				e.preventDefault();
				onInterrupt();
			}
		}
	};
	const restoreCycleDraft = () => {
		const savedDraft = cycleSavedDraft ?? "";
		setCycleIndex(null);
		setCycleSavedDraft(null);
		cycleHistorySnapshotRef.current = null;
		applyCycleValue(savedDraft);
	};

	const handleEditorKeyDown = (e: React.KeyboardEvent) => {
		if (e.key === "Escape" && cycleIndex !== null) {
			e.preventDefault();
			e.stopPropagation();
			restoreCycleDraft();
			return;
		}

		// isStreaming is intentionally excluded. Cycling is allowed while
		// streaming so the user can prepare the next prompt. Escape is
		// cycle-aware so it does not accidentally interrupt streaming.
		const isPromptCyclingSuppressed =
			isEditingHistoryMessage || isDisabled || isLoading;
		if (isPromptCyclingSuppressed) {
			return;
		}

		if (e.key !== "ArrowUp" && e.key !== "ArrowDown") {
			return;
		}

		if (cycleIndex === null) {
			if (e.key !== "ArrowUp" || !isComposerEffectivelyEmpty) {
				return;
			}
			const cycleHistory = [...userPromptHistory];
			const latestPrompt = cycleHistory[0];
			if (latestPrompt === undefined) {
				return;
			}
			e.preventDefault();
			cycleHistorySnapshotRef.current = cycleHistory;
			setCycleIndex(0);
			setCycleSavedDraft(internalRef.current?.getValue() ?? "");
			applyCycleValue(latestPrompt);
			return;
		}

		e.preventDefault();
		const cycleHistory = cycleHistorySnapshotRef.current ?? userPromptHistory;
		if (e.key === "ArrowDown") {
			if (cycleIndex === 0) {
				restoreCycleDraft();
				return;
			}
			const nextIndex = cycleIndex - 1;
			const nextPrompt = cycleHistory[nextIndex];
			if (nextPrompt === undefined) {
				restoreCycleDraft();
				return;
			}
			setCycleIndex(nextIndex);
			applyCycleValue(nextPrompt);
			return;
		}

		// ArrowUp: load an older prompt.
		const lastIndex = cycleHistory.length - 1;
		if (lastIndex < 0) {
			restoreCycleDraft();
			return;
		}
		const nextIndex = Math.min(cycleIndex + 1, lastIndex);
		if (nextIndex === cycleIndex) {
			return;
		}
		const nextPrompt = cycleHistory[nextIndex];
		if (nextPrompt === undefined) {
			restoreCycleDraft();
			return;
		}
		setCycleIndex(nextIndex);
		applyCycleValue(nextPrompt);
	};

	// A turn is already running (or interrupting): a submission here does not
	// start immediately, it joins this chat's FIFO queue and is dispatched
	// automatically once the current turn reaches a terminal state. This is
	// purely a submit-time label/placeholder distinction — the send call
	// itself, the queue, and its auto-dispatch are unchanged either way.
	const isQueueingSubmission = isStreaming && !isEditingHistoryMessage;
	const sendButtonLabel = isEditingHistoryMessage
		? "Save Edit"
		: isQueueingSubmission
			? "Queue"
			: "Send";
	const effectivePlaceholder = isQueueingSubmission
		? "Queue for after this turn..."
		: placeholder;
	const sendShortcutLabel =
		sendShortcut === MODIFIER_AGENT_CHAT_SEND_SHORTCUT
			? "Cmd/Ctrl+Enter"
			: "Enter";
	const sendButtonKeyShortcuts =
		sendShortcut === MODIFIER_AGENT_CHAT_SEND_SHORTCUT
			? "Control+Enter Meta+Enter"
			: "Enter";

	const content = (
		<div
			className={cn(
				"mx-auto w-full pb-0 sm:pb-4",
				chatWidthClass(chatFullWidth),
				isEditingHistoryMessage && "pt-1",
			)}
		>
			{queuedMessages.length > 0 && (
				<QueuedMessagesList
					messages={queuedMessages}
					onDelete={(id) => onDeleteQueuedMessage?.(id)}
					onPromote={(id) => onPromoteQueuedMessage?.(id)}
					className="mb-2"
				/>
			)}
			{showAgentSetupNotice && (
				<div className="relative z-0 mb-[-2.5rem]">
					{(aiGatewayDisabled ||
						(providerCount !== undefined && modelCount !== undefined)) &&
					canConfigureAgentSetup ? (
						<AgentSetupNotice
							isAdmin
							providerCount={providerCount ?? 0}
							modelCount={modelCount ?? 0}
							unsupportedProviderNames={unsupportedProviderNames}
							aiGatewayDisabled={aiGatewayDisabled}
						/>
					) : (
						<AgentSetupNotice
							isAdmin={false}
							providerCount={0}
							modelCount={0}
							unsupportedProviderNames={unsupportedProviderNames}
							aiGatewayDisabled={aiGatewayDisabled}
						/>
					)}
				</div>
			)}
			{(btwResult || btwPending || btwError) && (
				<div
					data-testid="btw-panel"
					className="relative z-10 mb-2 rounded-xl border border-border-default bg-surface-secondary/70 px-3 py-2 text-xs text-content-secondary"
				>
					<div className="flex items-start justify-between gap-2">
						<div className="flex items-center gap-1.5 font-medium text-content-primary">
							<MessageCircleQuestionIcon className="size-3.5" />
							Ask without interrupting
						</div>
						<Button
							type="button"
							variant="subtle"
							size="icon"
							aria-label="Dismiss"
							onClick={handleDismissBtw}
							className="size-5 rounded text-content-secondary hover:text-content-primary"
						>
							<XIcon className="size-3" />
						</Button>
					</div>
					{btwPending && (
						<div className="mt-1 flex items-center gap-1.5">
							<Spinner size="sm" loading aria-hidden="true" />
							Checking…
						</div>
					)}
					{btwError && !btwPending && (
						<p className="mt-1 text-content-destructive">{btwError}</p>
					)}
					{btwResult && !btwPending && !btwError && (
						<p className="mt-1 whitespace-pre-line text-content-primary">
							{btwResult.answer}
						</p>
					)}
				</div>
			)}
			<div
				ref={setComposerElement}
				data-testid="chat-composer"
				className={cn(
					"relative z-10 rounded-2xl bg-surface-secondary sm:bg-surface-secondary/45 p-1 shadow-sm has-[textarea:focus]:ring-2 has-[textarea:focus]:ring-content-link/40",
					showAgentSetupNotice && "sm:bg-surface-secondary",
					isDragging && "ring-2 ring-content-link/40",
					isEditingHistoryMessage &&
						"shadow-[0_0_0_2px_hsla(var(--border-warning),0.6)]",
				)}
				onKeyDown={handleComposerKeyDown}
				onDragOver={onAttach ? handleDragOver : undefined}
				onDragLeave={onAttach ? handleDragLeave : undefined}
				onDrop={onAttach ? handleDrop : undefined}
			>
				{isEditingHistoryMessage && (
					<div className="flex items-center justify-between border-b border-border-default/70 px-3 py-1.5">
						<span className="flex items-center gap-1.5 text-xs font-medium text-content-warning">
							<PencilIcon className="size-3.5" />
							Editing will delete all subsequent messages and restart the
							conversation here.
						</span>
						<Button
							type="button"
							variant="subtle"
							size="icon"
							aria-label="Cancel editing"
							onClick={onCancelHistoryEdit}
							disabled={isLoading}
							className="size-6 rounded text-content-warning hover:text-content-primary"
						>
							<XIcon className="size-3.5" />
						</Button>
					</div>
				)}
				{onRemoveAttachment && (
					<AttachmentPreview
						attachments={attachments}
						onRemove={onRemoveAttachment}
						uploadStates={uploadStates}
						previewUrls={previewUrls}
						onPreview={setPreviewImage}
						textContents={textContents}
						onTextPreview={handleTextPreview}
						onInlineText={handleInlineText}
					/>
				)}
				<ChatMessageInput
					ref={internalRef}
					onFilePaste={onAttach ? handleFilePaste : undefined}
					onPaste={resetPromptCycle}
					aria-label="Chat message"
					className="min-h-[60px] sm:min-h-24 w-full resize-none bg-transparent px-3 py-2 font-sans text-[13px] leading-relaxed text-content-primary placeholder:text-content-secondary disabled:cursor-not-allowed disabled:opacity-70"
					placeholder={effectivePlaceholder}
					initialValue={initialValue}
					initialEditorState={initialEditorState}
					remountKey={remountKey}
					onChange={handleContentChange}
					onKeyDown={handleEditorKeyDown}
					onEnter={handleSubmit}
					sendShortcut={sendShortcut}
					disabled={isDisabled || isLoading}
					hasWorkspace={hasSkillsWorkspace}
					workspaceSkills={workspaceSkills}
					autoFocus
					slashCommands={slashCommands}
					skillsMenuAnchor={composerElement}
				/>
				{/* Warn about invisible Unicode in the message text.
				 * Unlike the admin/user prompt textareas (which strip
				 * invisible chars server-side on save), the chat input
				 * is the user's free-form message; we don't silently
				 * mutate it. Instead we surface a warning so the user
				 * can make an informed decision. This guards against
				 * social engineering attacks where a user is tricked
				 * into pasting a "prompt" containing hidden LLM
				 * instructions encoded as zero-width characters. */}
				{invisibleCharCount > 0 && (
					<div className="px-3 pb-1">
						<Alert severity="warning">
							<AlertDescription>
								This message contains {invisibleCharCount} invisible Unicode
								character{invisibleCharCount !== 1 ? "s" : ""} that could hide
								content. Review carefully before sending.
							</AlertDescription>
						</Alert>
					</div>
				)}
				{/* Hidden file input for attaching any server-accepted file type. */}
				{onAttach && (
					<input
						ref={fileInputRef}
						type="file"
						multiple
						accept={chatAttachmentAcceptAttribute}
						onChange={handleFileSelect}
						className="hidden"
					/>
				)}
				<div className="flex items-center justify-between gap-2 px-2.5 pb-1.5">
					{/* flex-1 routes free row space to the growing pills. */}
					<div className="flex min-w-0 flex-1 items-center gap-1">
						{/* Plus menu */}
						<Popover
							modal={false}
							open={plusMenuOpen}
							onOpenChange={(open) => {
								setPlusMenuOpen(open);
								if (!open) setPlusMenuView("main");
							}}
						>
							{" "}
							<PopoverTrigger asChild>
								<Button
									type="button"
									variant="subtle"
									size="icon"
									className="size-7 shrink-0 rounded-full [&>svg]:!size-icon-sm [&>svg]:p-0"
									disabled={
										isDisabled &&
										!showAgentSetupNotice &&
										!canUseWorkspacePicker
									}
									aria-label="More options"
								>
									<PlusIcon />
								</Button>
							</PopoverTrigger>
							<PopoverContent
								side="bottom"
								align="start"
								className="mobile-full-width-dropdown mobile-full-width-dropdown-bottom w-auto min-w-[200px] p-1"
							>
								{plusMenuView === "workspace" ? (
									<div className="p-0">
										<button
											type="button"
											onClick={() => setPlusMenuView("main")}
											className="flex h-8 w-full cursor-pointer items-center gap-1.5 border-none bg-transparent px-1 text-xs text-content-secondary shadow-none transition-colors hover:text-content-primary"
										>
											<ArrowLeftIcon className="size-3.5 shrink-0" />
											<span>Back</span>
										</button>
										<Separator className="my-1" />
										<RepositoryWorkspacePickerList
											workspaceOptions={workspaceOptions}
											selectedWorkspaceId={selectedWorkspaceId}
											chatOrganizationId={chatOrganizationId}
											onSelect={(id) => {
												onWorkspaceChange?.(id);
												setPlusMenuOpen(false);
											}}
										/>
									</div>
								) : (
									<>
										{onAttach && (
											<button
												type="button"
												onClick={() => {
													resetPromptCycle();
													setPlusMenuOpen(false);
													fileInputRef.current?.click();
												}}
												className="group flex h-8 w-full cursor-pointer items-center gap-1.5 border-none bg-transparent px-1 text-xs text-content-secondary shadow-none transition-colors hover:text-content-primary"
											>
												<PaperclipIcon className="size-3.5 shrink-0" />
												Attach file
											</button>
										)}
										{onPlanModeToggle && (
											<button
												type="button"
												role="menuitemcheckbox"
												aria-checked={planModeEnabled}
												onClick={handlePlanModeToggle}
												disabled={isDisabled}
												className="group flex h-8 w-full cursor-pointer items-center gap-1.5 border-none bg-transparent px-1 text-xs text-content-secondary shadow-none transition-colors hover:text-content-primary disabled:cursor-not-allowed disabled:opacity-50"
											>
												<PencilIcon className="size-3.5 shrink-0" />
												<span>Plan first</span>
												{planModeEnabled && (
													<CheckIcon className="ml-auto size-icon-sm shrink-0" />
												)}
											</button>
										)}
										{workspaceOptions &&
											onWorkspaceChange &&
											(isBelowMdViewport() ? (
												<button
													type="button"
													disabled={!canUseWorkspacePicker}
													onClick={() => setPlusMenuView("workspace")}
													className="group flex h-8 w-full cursor-pointer items-center gap-1.5 border-none bg-transparent px-1 text-xs text-content-secondary shadow-none transition-colors hover:text-content-primary disabled:cursor-not-allowed disabled:opacity-50"
												>
													<MonitorIcon className="size-3.5 shrink-0" />
													<span>Attach workspace</span>
													<ChevronRightIcon className="ml-auto size-icon-sm" />
												</button>
											) : (
												<Popover
													open={workspacePickerOpen}
													onOpenChange={setWorkspacePickerOpen}
												>
													<PopoverTrigger asChild>
														<button
															type="button"
															disabled={!canUseWorkspacePicker}
															className="group flex h-8 w-full cursor-pointer items-center gap-1.5 border-none bg-transparent px-1 text-xs text-content-secondary shadow-none transition-colors hover:text-content-primary disabled:cursor-not-allowed disabled:opacity-50"
														>
															<MonitorIcon className="size-3.5 shrink-0" />
															<span>Attach workspace</span>
															<ChevronRightIcon
																className={cn(
																	"ml-auto size-icon-sm transition-transform",
																	workspacePickerOpen && "rotate-180",
																)}
															/>
														</button>
													</PopoverTrigger>
													<PopoverContent
														side="right"
														align="start"
														sideOffset={8}
														className="w-64 p-0"
													>
														<RepositoryWorkspacePickerList
															workspaceOptions={workspaceOptions}
															selectedWorkspaceId={selectedWorkspaceId}
															chatOrganizationId={chatOrganizationId}
															onSelect={(id) => {
																onWorkspaceChange(id);
																setWorkspacePickerOpen(false);
																setPlusMenuOpen(false);
															}}
														/>
													</PopoverContent>
												</Popover>
											))}
										{enabledMcpServers.length > 0 && (
											<>
												<Separator className="my-1" />
												{enabledMcpServers.map((server) => {
													const isForceOn = server.availability === "force_on";
													const isSelected =
														isForceOn ||
														(selectedMCPServerIds?.includes(server.id) ??
															false);
													const needsAuth =
														server.auth_type === "oauth2" &&
														!server.auth_connected;
													const isConnecting = mcpConnectingId === server.id;
													return (
														<div
															key={server.id}
															className="flex items-center gap-1.5 px-1 py-1.5"
														>
															{server.icon_url ? (
																<ExternalImage
																	src={server.icon_url}
																	alt=""
																	className="size-3.5 shrink-0 rounded-sm"
																/>
															) : (
																<ServerIcon className="size-3.5 shrink-0 text-content-secondary" />
															)}
															<span className="min-w-0 flex-1 truncate text-xs text-content-secondary">
																{server.display_name}
															</span>
															{needsAuth ? (
																<Button
																	variant="outline"
																	size="sm"
																	className="h-6 shrink-0 px-2 text-[10px] leading-none"
																	onClick={() => connectMCPServer(server.id)}
																	disabled={
																		isDisabled || mcpConnectingId !== null
																	}
																>
																	{isConnecting ? (
																		<Spinner loading className="h-2.5 w-2.5" />
																	) : null}
																	Auth
																</Button>
															) : (
																<>
																	{server.auth_type === "oauth2" && (
																		<Button
																			variant="subtle"
																			size="icon"
																			className="size-6 shrink-0 text-content-secondary [&>svg]:size-3"
																			onClick={() => {
																				setPlusMenuOpen(false);
																				setMcpDisconnectTarget(server);
																			}}
																			disabled={isDisabled}
																			aria-label={`Disconnect ${server.display_name}`}
																		>
																			<UnlinkIcon />
																		</Button>
																	)}
																	<Switch
																		size="sm"
																		checked={isSelected}
																		onCheckedChange={(checked) =>
																			handleMcpToggle(server.id, checked)
																		}
																		disabled={isDisabled || isForceOn}
																		aria-label={`${isSelected ? "Disable" : "Enable"} ${server.display_name}`}
																	/>
																</>
															)}
														</div>
													);
												})}
											</>
										)}
									</>
								)}
							</PopoverContent>
						</Popover>
						{isModelCatalogLoading ? (
							<Skeleton className="h-6 w-24 rounded" />
						) : (
							<ModelSelector
								value={selectedModel}
								onValueChange={onModelChange}
								options={modelOptions}
								disabled={isDisabled}
								placeholder={modelSelectorPlaceholder}
								className={cn(pillSizingClasses, "md:h-auto")}
								dropdownSide="top"
								dropdownAlign="start"
								enableMobileFullWidthDropdown
								reasoningEffort={reasoningEffort}
								onReasoningEffortChange={onReasoningEffortChange}
							/>
						)}
						{planModeEnabled && !shouldOverflowPlanningBadge && (
							<span
								data-testid="planning-badge"
								className="hidden shrink-0 items-center gap-1 rounded-full bg-surface-secondary px-2 py-0.5 text-xs font-medium text-content-secondary sm:inline-flex"
							>
								<PencilIcon className="size-3" />
								Planning
								{onPlanModeToggle && (
									<BadgeDismissButton
										onClick={handleDisablePlanMode}
										ariaLabel="Disable plan mode"
										isDisabled={isDisabled}
									/>
								)}
							</span>
						)}
						{/* Badges and the +N pill stay mounted for measurement:
						 * overflowed badges are display:none, the pill merely
						 * invisible so its width stays readable. */}
						<div
							ref={badgeContainerRef}
							className="flex min-w-0 items-center gap-1 overflow-hidden"
						>
							{allBadges.map((badge, i) => {
								const isOverflow = overflowCount > 0 && i >= visibleCount;
								if (
									badge === workspacePillBadge &&
									workspace &&
									workspaceAgent &&
									chatId
								) {
									return (
										<span
											key="workspace-pill"
											className={cn(
												"flex min-w-0 text-xs",
												pillSizingClasses,
												isOverflow && "hidden",
											)}
										>
											<WorkspacePill
												workspace={workspace}
												agent={workspaceAgent}
												chatId={chatId}
												sshCommand={sshCommand}
												folder={folder}
												onRemoveWorkspace={removeWorkspaceHandler}
											/>
										</span>
									);
								}
								return (
									<ToolBadge
										key={badge.kind === "mcp" ? badge.server.id : badge.kind}
										badge={badge}
										onRemoveWorkspace={removeWorkspaceHandler}
										onRemoveMcp={handleRemoveMcp}
										onRemovePlanning={
											onPlanModeToggle ? handleDisablePlanMode : undefined
										}
										isDisabled={isDisabled}
										className={isOverflow ? "hidden" : undefined}
									/>
								);
							})}
							<Popover
								open={overflowPopoverOpen && overflowCount > 0}
								onOpenChange={setOverflowPopoverOpen}
							>
								<PopoverTrigger asChild>
									<button
										type="button"
										className={cn(
											"inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-full border-0 bg-surface-secondary px-2 py-0.5 text-xs font-medium text-content-secondary transition-colors hover:bg-surface-tertiary hover:text-content-primary",
											overflowCount === 0 && "invisible",
										)}
										aria-label={`${overflowCount} more item${overflowCount !== 1 ? "s" : ""}`}
										aria-hidden={overflowCount === 0}
									>
										+{overflowCount}
									</button>
								</PopoverTrigger>
								{/* Anchored above the +N pill; hugs the toolbar row. */}
								<PopoverContent
									side="top"
									align="start"
									className="flex w-auto max-w-64 flex-wrap gap-1 p-2"
									onInteractOutside={(event) => {
										// The workspace pill portals its menu outside
										// this popover; dismissing would unmount the
										// open menu. Ignore focus shifts and pointer
										// presses inside the menu.
										if (event.detail.originalEvent.type !== "pointerdown") {
											event.preventDefault();
											return;
										}
										if (
											event.target instanceof Element &&
											event.target.closest('[role="menu"]')
										) {
											event.preventDefault();
										}
									}}
								>
									{overflowBadges.map((badge, i) => {
										if (
											badge === workspacePillBadge &&
											workspace &&
											workspaceAgent &&
											chatId
										) {
											return (
												<span
													key="workspace-pill-overflow"
													className="flex min-w-0 text-xs"
												>
													<WorkspacePill
														workspace={workspace}
														agent={workspaceAgent}
														chatId={chatId}
														sshCommand={sshCommand}
														folder={folder}
														onRemoveWorkspace={removeWorkspaceHandler}
														inOverflowPopover
													/>
												</span>
											);
										}
										return (
											<ToolBadge
												// Non-MCP badges can share a kind, so keys
												// are position-qualified.
												key={
													badge.kind === "mcp"
														? badge.server.id
														: `${badge.kind}-overflow-${visibleCount + i}`
												}
												badge={badge}
												onRemoveWorkspace={removeWorkspaceHandler}
												onRemoveMcp={handleRemoveMcp}
												onRemovePlanning={
													onPlanModeToggle ? handleDisablePlanMode : undefined
												}
												isDisabled={isDisabled}
												disableTooltip
											/>
										);
									})}
								</PopoverContent>
							</Popover>
						</div>
						<button
							type="button"
							onClick={handleSkillsMenuButton}
							disabled={isDisabled || isLoading}
							aria-label="Tools: skills and commands"
							title="Tools: skills and commands (/)"
							className="inline-flex h-6 shrink-0 cursor-pointer items-center rounded-full border-0 bg-surface-secondary px-2.5 text-xs font-medium text-content-secondary transition-colors hover:bg-surface-tertiary hover:text-content-primary disabled:cursor-not-allowed disabled:opacity-70"
						>
							Tools
						</button>
					</div>
					<div className="flex shrink-0 items-center gap-2">
						{speech.isSupported && (
							<>
								<Button
									type="button"
									variant="subtle"
									size="icon"
									className="size-7 shrink-0 rounded-full [&>svg]:!size-icon-sm [&>svg]:p-0"
									onClick={
										speech.isRecording
											? handleCancelRecording
											: handleStartRecording
									}
									disabled={isDisabled || speech.isTranscribing}
									aria-label={
										speech.isRecording ? "Cancel voice input" : "Voice input"
									}
								>
									{speech.isTranscribing ? (
										<Spinner size="sm" loading aria-hidden="true" />
									) : speech.isRecording ? (
										<XIcon />
									) : (
										<MicIcon strokeWidth={1.5} />
									)}
								</Button>
								{speech.isTranscribing && (
									<span className="text-2xs text-content-secondary">
										Transcrevendo...
									</span>
								)}
								{speech.error &&
									!speech.isRecording &&
									!speech.isTranscribing && (
										<span
											className="text-2xs text-content-destructive"
											role="alert"
										>
											{speech.error === "not-allowed"
												? "Mic access denied"
												: "Transcription failed"}
										</span>
									)}
							</>
						)}
						{contextUsage !== undefined && (
							<div
								className={cn(
									"flex",
									speech.isSupported && !speech.error && "-ml-2",
								)}
							>
								<ContextUsageIndicator
									usage={
										contextEstimate
											? {
													...(contextUsage ?? {}),
													usedTokens: contextEstimate.usedTokens,
													contextLimitTokens: contextEstimate.limitTokens,
													isEstimated: contextEstimate.isEstimated,
												}
											: contextUsage
									}
									onRefreshContext={onRefreshContext}
									isRefreshingContext={isRefreshingContext}
								/>
							</div>
						)}
						{isStreaming && onInterrupt && (
							<Tooltip>
								<TooltipTrigger asChild>
									<Button
										size="icon"
										variant="default"
										className="size-7 rounded-full transition-colors [&>svg]:!size-3 [&>svg]:p-0"
										onClick={onInterrupt}
										disabled={isInterruptPending}
									>
										<SquareIcon className="fill-current" />
										<span className="sr-only">Stop</span>
									</Button>
								</TooltipTrigger>
								<TooltipContent side="top">
									{isInterruptPending ? "Interrupting…" : "Stop"}
								</TooltipContent>
							</Tooltip>
						)}
						{isInterruptPending && isStreaming && (
							// The disabled Stop button is skipped by Tab order, so the
							// pending interruption is also announced through a live
							// region and a tooltip.
							<span role="status" className="sr-only">
								Interrupting. Waiting for the agent to stop.
							</span>
						)}
						{isStreaming && chatId && (
							<Tooltip>
								<TooltipTrigger asChild>
									<Button
										size="icon"
										variant="subtle"
										className="size-7 rounded-full transition-colors [&>svg]:!size-3 [&>svg]:p-0"
										onClick={() => handleSideQuery("")}
										disabled={btwPending}
									>
										{btwPending ? (
											<Spinner size="sm" loading aria-hidden="true" />
										) : (
											<MessageCircleQuestionIcon />
										)}
										<span className="sr-only">Ask without interrupting</span>
									</Button>
								</TooltipTrigger>
								<TooltipContent side="top">Ask without interrupting</TooltipContent>
							</Tooltip>
						)}
						{/* Voice input (above) and submission (below) both stay available
							while streaming: a turn already running still accepts a new
							prompt -- recorded or typed -- it just joins the queue instead
							of starting immediately, so this button stays alongside Stop. */}
						{(!isStreaming || isQueueingSubmission) && (
							<Tooltip>
								<TooltipTrigger asChild>
									<Button
										size="icon"
										variant={isQueueingSubmission ? "subtle" : "default"}
										className="size-7 rounded-full transition-colors [&>svg]:!size-5 [&>svg]:p-0"
										onClick={
											speech.isRecording ? handleAcceptRecording : handleSubmit
										}
										disabled={
											speech.isRecording
												? false
												: speech.isTranscribing || !canSend
										}
										aria-keyshortcuts={sendButtonKeyShortcuts}
									>
										{isLoading || speech.isTranscribing ? (
											<Spinner size="sm" loading aria-hidden="true" />
										) : speech.isRecording ? (
											<CheckIcon />
										) : (
											<ArrowUpIcon />
										)}
										<span className="sr-only">
											{speech.isRecording
												? "Accept voice input"
												: sendButtonLabel}
										</span>
									</Button>
								</TooltipTrigger>
								<TooltipContent side="top">
									{speech.isRecording
										? "Accept voice input"
										: speech.isTranscribing
											? "Transcrevendo..."
											: `${sendButtonLabel}: ${sendShortcutLabel}`}
								</TooltipContent>
							</Tooltip>
						)}
					</div>
				</div>
			</div>
		</div>
	);

	return (
		<>
			{content}
			{previewImage && (
				<ImageLightbox
					src={previewImage}
					onClose={() => setPreviewImage(null)}
				/>
			)}
			{previewText !== null && (
				<TextPreviewDialog
					content={previewText}
					fileName={previewTextFileName ?? undefined}
					mediaType={previewTextMediaType ?? undefined}
					onClose={() => {
						setPreviewText(null);
						setPreviewTextFileName(null);
						setPreviewTextMediaType(null);
					}}
				/>
			)}
			<ConfirmDialog
				open={mcpDisconnectTarget !== null}
				title={`Disconnect ${mcpDisconnectTarget?.display_name ?? "MCP server"}?`}
				description="This removes your credentials for this MCP server from Coder. You can authenticate again later."
				type="delete"
				confirmText="Disconnect"
				confirmLoading={mcpDisconnectMutation.isPending}
				onConfirm={handleMcpDisconnectConfirm}
				onClose={() => setMcpDisconnectTarget(null)}
			/>
		</>
	);
};

/**
 * Shared workspace picker used by both the mobile and desktop
 * "Attach workspace" menus. Workspaces from a different organization
 * than the chat are disabled unless already selected, so stale bindings
 * can still be cleared.
 */
export interface WorkspacePickerListProps {
	workspaceOptions:
		| ReadonlyArray<{
				id: string;
				name: string;
				organization_id: string;
		  }>
		| undefined;
	selectedWorkspaceId?: string | null;
	chatOrganizationId?: string;
	onSelect: (id: string | null) => void;
}

const WorkspacePickerList: FC<WorkspacePickerListProps> = ({
	workspaceOptions,
	selectedWorkspaceId,
	chatOrganizationId,
	onSelect,
}) => {
	return (
		<Command loop>
			<CommandInput placeholder="Search workspaces..." className="text-xs" />
			<CommandList>
				<CommandEmpty className="text-xs">No workspaces found</CommandEmpty>
				<CommandGroup>
					{workspaceOptions?.map((workspace) => {
						const isCrossOrg =
							!!chatOrganizationId &&
							workspace.organization_id !== chatOrganizationId;
						const isSelected = selectedWorkspaceId === workspace.id;
						const isUnavailable = isCrossOrg && !isSelected;

						const item = (
							<CommandItem
								className={cn(
									"text-xs font-normal",
									isUnavailable &&
										"cursor-not-allowed opacity-50 data-[disabled=true]:pointer-events-auto",
								)}
								key={workspace.id}
								value={workspace.name}
								disabled={isUnavailable}
								onSelect={() => {
									if (!isUnavailable) {
										onSelect(isSelected ? null : workspace.id);
									}
								}}
							>
								{workspace.name}
								{isSelected && (
									<CheckIcon className="ml-auto size-icon-sm shrink-0" />
								)}
							</CommandItem>
						);

						if (isUnavailable) {
							return (
								<Tooltip key={workspace.id}>
									<TooltipTrigger asChild>
										<div>{item}</div>
									</TooltipTrigger>
									<TooltipContent side="top">
										Chat and workspace must be in the same organization
									</TooltipContent>
								</Tooltip>
							);
						}

						return item;
					})}
				</CommandGroup>
			</CommandList>
		</Command>
	);
};

/**
 * Repository-driven "Attach workspace" picker (see WorkspacePickerList doc
 * comment above for the shared component this wraps). Lists the deployment's
 * configured GitHub organization repositories instead of raw workspace
 * names — selecting a repository reuses the caller's existing workspace for
 * it if one exists, or lazily creates one via POST
 * /api/v2/repository-catalog/attach, then calls onSelect with the resulting
 * workspace id exactly as the plain picker does. Falls back to the plain
 * WorkspacePickerList, unchanged, when the deployment has not configured the
 * repository catalog (GET .../repositories 404s) — this feature is
 * additive, not a replacement, for deployments that have not opted in.
 */
export const RepositoryWorkspacePickerList: FC<WorkspacePickerListProps> = ({
	workspaceOptions,
	selectedWorkspaceId,
	chatOrganizationId,
	onSelect,
}) => {
	const queryClient = useQueryClient();
	// Enabled unconditionally: this component only mounts while the Attach
	// Workspace menu is open, so the query itself is already "on demand."
	const catalogQuery = useQuery(repositoryCatalog(true));
	const attachMutation = useMutation(attachRepositoryWorkspace(queryClient));
	const [attachingRepoId, setAttachingRepoId] = useState<number | null>(null);
	// Repositories is the default so every existing repository-catalog flow
	// (and the tests covering it) is unaffected. "Existing workspaces" is an
	// explicit opt-in for workspaces that intentionally aren't modeled around
	// a single repo (e.g. a multi-repo administrative workspace) and so never
	// show up in the repository-driven list above, no matter how the
	// catalog is configured.
	const [activeTab, setActiveTab] = useState<"repositories" | "workspaces">(
		"repositories",
	);

	const notConfigured =
		isAxiosError(catalogQuery.error) &&
		catalogQuery.error.response?.status === 404;
	if (notConfigured) {
		return (
			<WorkspacePickerList
				workspaceOptions={workspaceOptions}
				selectedWorkspaceId={selectedWorkspaceId}
				chatOrganizationId={chatOrganizationId}
				onSelect={onSelect}
			/>
		);
	}

	if (catalogQuery.isLoading) {
		return (
			<div className="flex items-center justify-center p-4">
				<Spinner loading />
			</div>
		);
	}

	if (catalogQuery.isError) {
		return (
			<div className="flex flex-col items-center gap-2 p-3 text-center text-xs text-content-secondary">
				<span>Couldn't load repositories from GitHub.</span>
				<Button
					size="sm"
					variant="outline"
					onClick={() => catalogQuery.refetch()}
				>
					Retry
				</Button>
			</div>
		);
	}

	const repositories = catalogQuery.data?.repositories ?? [];

	const handleSelectRepository = async (repo: Repository) => {
		if (repo.workspace_id) {
			onSelect(repo.workspace_id);
			return;
		}
		setAttachingRepoId(repo.id);
		// No try/finally here on purpose: the React Compiler (enabled for this
		// directory, see scripts/check-compiler.mjs) cannot yet lower a
		// TryStatement with a finalizer. Both the success and error paths
		// already fall through to the same setAttachingRepoId(null) below,
		// so this is behaviorally identical to a finally block.
		try {
			const result = await attachMutation.mutateAsync({
				owner: repo.owner,
				repo: repo.name,
			});
			onSelect(result.workspace.id);
		} catch (error) {
			toast.error(
				getErrorMessage(
					error,
					`Failed to create a workspace for ${repo.full_name}.`,
				),
			);
		}
		setAttachingRepoId(null);
	};

	return (
		<div className="flex flex-col">
			<div className="flex gap-1 border-b border-border-default p-1">
				<Button
					size="xs"
					variant={activeTab === "repositories" ? "outline" : "subtle"}
					className="flex-1"
					onClick={() => setActiveTab("repositories")}
				>
					Repositories
				</Button>
				<Button
					size="xs"
					variant={activeTab === "workspaces" ? "outline" : "subtle"}
					className="flex-1"
					onClick={() => setActiveTab("workspaces")}
				>
					Existing workspaces
				</Button>
			</div>
			{activeTab === "workspaces" ? (
				<WorkspacePickerList
					workspaceOptions={workspaceOptions}
					selectedWorkspaceId={selectedWorkspaceId}
					chatOrganizationId={chatOrganizationId}
					onSelect={onSelect}
				/>
			) : (
				<Command loop>
					<CommandInput
						placeholder="Search repositories..."
						className="text-xs"
					/>
					<CommandList>
						<CommandEmpty className="text-xs">
							No repositories found
						</CommandEmpty>
						<CommandGroup>
							{repositories.map((repo) => {
								const isAttached = repo.workspace_id === selectedWorkspaceId;
								const isAttaching = attachingRepoId === repo.id;
								const statusLabel = isAttached
									? "Attached"
									: repo.workspace_id
										? "Ready"
										: "Not created";

								return (
									<CommandItem
										className="text-xs font-normal"
										key={repo.id}
										value={repo.name}
										disabled={isAttaching}
										onSelect={() => {
											void handleSelectRepository(repo);
										}}
									>
										<span className="min-w-0 flex-1 truncate">{repo.name}</span>
										{isAttaching ? (
											<Spinner loading className="size-3 shrink-0" />
										) : (
											<span className="shrink-0 text-content-secondary">
												{statusLabel}
											</span>
										)}
										{isAttached && (
											<CheckIcon className="ml-auto size-icon-sm shrink-0" />
										)}
									</CommandItem>
								);
							})}
						</CommandGroup>
					</CommandList>
				</Command>
			)}
		</div>
	);
};
