----------------------- MODULE VerifierHandoff -----------------------
EXTENDS Naturals
CONSTANTS MaxRounds, OriginalDeadline, Bug
VARIABLE s
vars == <<s>>

\* One origin chain. I0 abstracts the exact origin/source/candidate/proposal/
\* fixture identities; I1 differs in at least one component. H0/H1 are distinct
\* verifier heads. Discrete time includes a late-event state after the deadline.
Init == s = [phase |-> "upgrade", rounds |-> 0, spent |-> 0,
  now |-> 0, deadline |-> OriginalDeadline, handoffs |-> 0,
  inputs |-> "I0", head |-> "H0", proof |-> "missing",
  proofHead |-> "none", proofInputs |-> "none",
  reviewer |-> "none", approved |-> FALSE, reviewHead |-> "none",
  reviewInputs |-> "none", publications |-> 0,
  publishedHead |-> "none", publishedReviewer |-> "none",
  publishedProof |-> "missing", ci |-> "missing", ciHead |-> "none",
  mergedHead |-> "none", mainHead |-> "B0", consumed |-> FALSE,
  continuations |-> 0, controllerEpoch |-> 0, controllerHead |-> "B0",
  authorizedHead |-> "none", authorizedInputs |-> "none",
  authorizedLiveInputs |-> "none",
  authorizedMain |-> "none", authorizedAt |-> 0]

Live == s.now < s.deadline
ExactInputs == s.inputs = "I0"
MayConstruct == Live /\ ExactInputs /\ s.rounds < MaxRounds
BoundProof == /\ s.proof = "passed"
              /\ s.proofHead = s.head
              /\ s.proofInputs = "I0"
BoundReview == /\ s.approved
               /\ s.reviewHead = s.head
               /\ s.reviewInputs = "I0"
               /\ (s.reviewer = "peer" \/ Bug = "self-review")

UpgradeProposal == /\ s.phase = "upgrade"
                   /\ MayConstruct
                   /\ s' = [s EXCEPT !.phase = "verify",
                             !.rounds = @ + 1, !.spent = @ + 1]
ImplementationFailure == /\ s.phase = "verify"
                         /\ s' = [s EXCEPT !.phase = "upgrade"]
SuspectVerifier == /\ s.phase = "verify"
                  /\ (s.handoffs = 0 \/ Bug = "repeat-handoff")
                  /\ s' = [s EXCEPT !.phase = "verifier"]
VerifierProposal == /\ s.phase = "verifier"
                    /\ MayConstruct
                    /\ s' = [s EXCEPT !.phase = "review",
                              !.rounds = @ + 1, !.spent = @ + 1,
                              !.handoffs = @ + 1, !.proof = "missing",
                              !.approved = FALSE, !.reviewer = "none",
                              !.deadline = IF Bug = "refresh-deadline"
                                           THEN s.now + OriginalDeadline
                                           ELSE @]
\* This is the fixed runner's observed result, not the author's claimed result.
ExecuteProof(result) == /\ s.phase = "review"
                        /\ s.proof = "missing"
                        /\ s' = [s EXCEPT !.proof = result,
                                  !.proofHead = s.head,
                                  !.proofInputs = s.inputs]
PeerReview(who, head, inputs, approved) ==
  /\ s.phase = "review"
  /\ s.reviewer = "none"
  /\ s' = [s EXCEPT !.reviewer = who, !.reviewHead = head,
                    !.reviewInputs = inputs, !.approved = approved]
Publish == /\ s.phase = "review"
           /\ Live /\ ExactInputs /\ BoundProof /\ BoundReview
           /\ s' = [s EXCEPT !.phase = "published",
                     !.publications = @ + 1, !.publishedHead = s.head,
                     !.publishedReviewer = s.reviewer,
                     !.publishedProof = s.proof, !.ci = "pending"]
Checks(head, result) == /\ s.phase = "published"
                       /\ s.ci = "pending"
                       /\ s' = [s EXCEPT !.ci = result, !.ciHead = head]
\* Normal protected integration is abstracted as an exact-head successful gate.
\* Integration itself need not finish within the original repair deadline.
Merge == /\ s.phase = "published"
         /\ ExactInputs /\ BoundProof /\ BoundReview
         /\ s.ci = "passed" /\ s.ciHead = s.head
         /\ s.publishedHead = s.head
         /\ s' = [s EXCEPT !.phase = "merged", !.mergedHead = s.head,
                           !.mainHead = s.head]

\* Every received event is untrusted routing data. Valid and stale/duplicate
\* deliveries use the same action. Rejected deliveries are stuttering steps.
DeliverMergedEvent(head, inputs) ==
  IF /\ s.mergedHead # "none"
     /\ s.phase \in {"merged", "recheck", "upgrade", "done"}
     /\ (Live \/ Bug = "late-event")
     /\ (ExactInputs \/ Bug = "stale-inputs") /\ inputs = "I0"
     /\ s.mainHead = s.mergedHead
     /\ (head = s.mergedHead \/ Bug = "stale-head")
     /\ (~s.consumed \/ Bug = "duplicate")
  THEN s' = [s EXCEPT !.phase = "recheck", !.consumed = TRUE,
             !.continuations = @ + 1, !.authorizedHead = head,
             !.authorizedInputs = inputs, !.authorizedLiveInputs = s.inputs,
             !.authorizedMain = s.mainHead,
             !.authorizedAt = s.now,
             !.controllerEpoch = IF Bug = "reuse-controller" THEN 0 ELSE 1,
             !.controllerHead = IF Bug = "reuse-controller" THEN "B0" ELSE head,
             !.rounds = IF Bug = "reset-budget" THEN 0 ELSE @]
  ELSE UNCHANGED s
\* Retained proposal is rechecked without a new model turn or round charge.
FreshRecheck(result) == /\ s.phase = "recheck"
                        /\ Live /\ ExactInputs
                        /\ s' = [s EXCEPT !.phase = IF result = "passed"
                                                  THEN "done" ELSE "upgrade"]

MoveInputs == /\ s.inputs = "I0"
              /\ s' = [s EXCEPT !.inputs = "I1"]
MoveHead == /\ s.phase \in {"review", "published"}
            /\ s.head = "H0"
            /\ s' = [s EXCEPT !.head = "H1"]
MoveMain == /\ s.mergedHead # "none"
            /\ s.mainHead # "other"
            /\ s' = [s EXCEPT !.mainHead = "other"]
Tick == /\ s.now <= OriginalDeadline
        /\ s' = [s EXCEPT !.now = @ + 1]

Next == UpgradeProposal \/ ImplementationFailure \/ SuspectVerifier
        \/ VerifierProposal \/ Publish \/ Merge \/ MoveInputs \/ MoveHead
        \/ MoveMain \/ Tick
        \/ (\E result \in {"passed", "failed"}: ExecuteProof(result))
        \/ (\E who \in {"author", "peer"}, head \in {"H0", "H1"},
               inputs \in {"I0", "I1"}, approved \in BOOLEAN:
               PeerReview(who, head, inputs, approved))
        \/ (\E head \in {"H0", "H1"}, result \in {"passed", "failed"}:
               Checks(head, result))
        \/ (\E head \in {"H0", "H1"}, inputs \in {"I0", "I1"}:
               DeliverMergedEvent(head, inputs))
        \/ (\E result \in {"passed", "failed"}: FreshRecheck(result))
        \/ UNCHANGED vars
Spec == Init /\ [][Next]_vars

TypeOK == /\ s.phase \in {"upgrade", "verify", "verifier", "review",
                          "published", "merged", "recheck", "done"}
          /\ s.rounds \in Nat /\ s.spent \in Nat
          /\ s.now \in 0..(OriginalDeadline + 1)
          /\ s.handoffs \in Nat /\ s.continuations \in Nat
SharedBudget == /\ s.rounds = s.spent
                /\ s.spent <= MaxRounds
DeadlineNeverRefreshes == s.deadline = OriginalDeadline
OneHandoff == s.handoffs <= 1
IndependentPublication == s.publications > 0 =>
                           (s.publishedReviewer = "peer" /\ s.publishedProof = "passed")
OneAuthorization == s.continuations <= 1
ExactFreshContinuation == s.continuations > 0 =>
  /\ s.authorizedHead = s.mergedHead
  /\ s.authorizedMain = s.mergedHead
  /\ s.authorizedInputs = "I0"
  /\ s.authorizedLiveInputs = "I0"
  /\ s.controllerHead = s.mergedHead
  /\ s.controllerEpoch = 1
NoLateAuthorization == s.continuations > 0 => s.authorizedAt < OriginalDeadline
=============================================================================
