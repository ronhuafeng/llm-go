------------------------- MODULE SyncLifecycle -------------------------
EXTENDS Naturals
CONSTANTS MaxRounds, BugOldGreen
VARIABLE s
vars == <<s>>

Init == s = [phase |-> "construct", base |-> "B1", target |-> "U1",
  head |-> "H1", candidateBase |-> "B1", candidateTarget |-> "U1",
  candidateHead |-> "H1", round |-> 0, feedback |-> 0,
  modelDone |-> FALSE, verified |-> FALSE, attempt |-> 1,
  checkHead |-> "H1", trustedApp |-> TRUE, latest |-> "missing",
  oldGreen |-> FALSE, liveStarted |-> FALSE, recoveryUsed |-> FALSE,
  acceptedAttempt |-> 0]

SameCandidate == /\ s.base = s.candidateBase
                 /\ s.target = s.candidateTarget
                 /\ s.head = s.candidateHead
FreshProof == /\ s.latest = "succeeded"
              /\ s.checkHead = s.head
              /\ s.trustedApp

Construct == /\ s.phase = "construct"
             /\ s' = [s EXCEPT !.phase = "model", !.round = 1]
ModelReply == /\ s.phase = "model"
              /\ s' = [s EXCEPT !.phase = "verify", !.modelDone = TRUE]
VerifyGood == /\ s.phase = "verify"
              /\ SameCandidate
              /\ s' = [s EXCEPT !.phase = "ready", !.verified = TRUE]
VerifyBad == /\ s.phase = "verify"
             /\ SameCandidate
             /\ s' = IF s.round < MaxRounds
                  THEN [s EXCEPT !.phase = "model", !.round = @ + 1,
                                 !.feedback = s.round, !.modelDone = FALSE]
                  ELSE [s EXCEPT !.phase = "rejected"]
CancelRepair == /\ s.phase = "model"
                /\ s' = [s EXCEPT !.phase = "cancelled"]
ResumeRepair == /\ s.phase = "cancelled"
                /\ SameCandidate
                /\ s.round < MaxRounds
                /\ s' = [s EXCEPT !.phase = "model", !.round = @ + 1]
RejectExhausted == /\ s.phase = "cancelled"
                   /\ s.round = MaxRounds
                   /\ s' = [s EXCEPT !.phase = "rejected"]
Publish == /\ s.phase = "ready"
           /\ SameCandidate
           /\ s.verified
           /\ s' = [s EXCEPT !.phase = "published"]
MoveBase == /\ s.phase \notin {"accepted", "rejected"}
            /\ s.base = "B1"
            /\ s' = [s EXCEPT !.base = "B2"]
MoveTarget == /\ s.phase \notin {"accepted", "rejected"}
              /\ s.target = "U1"
              /\ s' = [s EXCEPT !.target = "U2"]
MoveHead == /\ s.phase \notin {"accepted", "rejected"}
            /\ s.head = "H1"
            /\ s' = [s EXCEPT !.head = "H2"]
RejectStale == /\ s.phase \notin {"accepted", "rejected"}
               /\ ~SameCandidate
               /\ s' = [s EXCEPT !.phase = "rejected"]

StartChecks == /\ s.phase = "published"
               /\ s.latest = "missing"
               /\ s' = [s EXCEPT !.latest = "pending"]
LiveStart == /\ s.phase = "published"
             /\ s.latest = "pending"
             /\ ~s.liveStarted
             /\ s' = [s EXCEPT !.liveStarted = TRUE]
CheckResult(h, app, result) ==
  /\ s.phase = "published"
  /\ s.latest = "pending"
  /\ s' = [s EXCEPT !.latest = result, !.checkHead = h, !.trustedApp = app]

\* External/manual rerun can supersede an old success before creating any job.
NewAttempt == /\ s.phase = "published"
              /\ s.attempt = 1
              /\ FreshProof
              /\ s' = [s EXCEPT !.attempt = 2, !.latest = "pending",
                                !.oldGreen = TRUE, !.liveStarted = FALSE]
\* Automated recovery never repeats a started live execution.
RecoverChecks == /\ s.phase = "published"
                 /\ SameCandidate
                 /\ s.latest = "cancelled"
                 /\ s.attempt = 1
                 /\ ~s.liveStarted
                 /\ s' = [s EXCEPT !.attempt = 2, !.latest = "pending",
                                   !.recoveryUsed = TRUE]
Accept == /\ s.phase = "published"
          /\ SameCandidate
          /\ s.verified
          /\ (FreshProof \/ (BugOldGreen /\ s.oldGreen))
          /\ s' = [s EXCEPT !.phase = "accepted", !.acceptedAttempt = s.attempt]

Next == Construct \/ ModelReply \/ VerifyGood \/ VerifyBad \/ CancelRepair
        \/ ResumeRepair \/ RejectExhausted \/ Publish \/ MoveBase \/ MoveTarget
        \/ MoveHead \/ RejectStale \/ StartChecks \/ LiveStart \/ NewAttempt
        \/ RecoverChecks \/ Accept
        \/ (\E h \in {"H1", "H2"}, app \in BOOLEAN,
                result \in {"succeeded", "failed", "cancelled"}:
                CheckResult(h, app, result))
        \/ UNCHANGED vars

Spec == Init /\ [][Next]_vars
TypeOK == /\ s.phase \in {"construct", "model", "verify", "ready",
                          "published", "cancelled", "accepted", "rejected"}
          /\ s.round \in 0..MaxRounds
          /\ s.feedback \in 0..MaxRounds
          /\ s.attempt \in 1..2
          /\ s.latest \in {"missing", "pending", "succeeded", "failed", "cancelled"}
CandidateBound == s.phase = "accepted" => SameCandidate
ProgramOwnsAcceptance == s.phase \in {"ready", "published", "accepted"} => s.verified
LatestExactProof == s.phase = "accepted" =>
                     (FreshProof /\ s.acceptedAttempt = s.attempt)
RepairBudget == s.round <= MaxRounds
FeedbackPrecedesNextRound == s.feedback > 0 => s.feedback < s.round
RecoveryBound == s.recoveryUsed => s.attempt = 2
=============================================================================
