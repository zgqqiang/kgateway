# Pull Request Reviews

This doc explains the best practices for reviewing a pull request in the [kgateway project](https://github.com/kgateway-dev/kgateway).
If you are looking to contribute to the project, check out the [writing pull requests guide](pull-requests.md).

- [Reviewing Pull Requests](#reviewing-pull-requests)
- [Submitting Reviews](#submitting-reviews)
  - [Ask questions](#ask-questions)
  - [Clarify Nits](#clarify-nits)
  - [Requesting Changes](#requesting-changes)
  - [Providing Guidance](#providing-guidance)
  - [Checking out the code](#checking-out-the-code)
  - [Run tests](#run-tests)
- [Approving Pull Requests](#approving-pull-requests)

## Reviewing Pull Requests
- First, read the description of the pull request to find additional information or context that the author has given. This information can help answer questions as you parse through the changes. 
- Look at the tests to see what use cases are being covered and what guarantees are being made. If you cannot fully verify the change or code flow based on the tests, consider requesting more.  
- Verify the semantics of variable names by checking how they are assigned and used. If variable names do not make sense or do not fit into the context where they are used, get clarification from the author. 

## Submitting Reviews
### Ask questions
You are encouraged to ask questions to fully understand the impact of the changes in a PR. If something isn't clear to you, it is recommended to reach out to the author, by commenting on the PR, to get clarification.  

When asking questions, try to be mindful with your phrasing. Instead of:

_"Why did you do this?"_

try

_"Am I understanding this correctly? Can you explain why...?"_

Internal developers should not be shy about posting questions on the issue rather than in a private message. Having these conversations on the issue itself is valuable for increasing visibility both for other reviewers and future developers trying to understand past decision-making. Conversations that occur in Slack can be summarized and linked.

### Identify Nits
Use the prefix “nit” when making a comment about something nitpicky, such as code style/formatting, variable names, etc. Nits express your opinion that something could be better, but that it’s not critical or necessary for correctness or system health. The author can choose to ignore such feedback, but it’s still useful to provide it.

When you identify a comment as a nit, you are delegating "ownernship" of that conversation to the PR author. That means that they have the ability to respond to the proposal, implement a change, or just resolve the conversation without taking any action.

### Requesting Changes
Be mindful of the manner (especially the language) in which you deliver critical feedback, especially feedback that may require the author to make significant changes. You might also consider reaching out to the author directly to discuss things offline after submitting the review. 

_Don’t be afraid to give critical feedback, whether it is technical feedback on a PR or interpersonal feedback about behavior. We assume good intent and remember that we are on the same team._

### Providing Guidance
When requesting a change, provide clear guidance on how it should be done instead. This can include direct suggestions in the PR review, links to a GitHub issue, a Slack conversation, a design doc, or another project's codebase.

Avoid broad statements that do not enable the author to iterate effectively.

### Checking out the code
Sometimes diffs in the GitHub UI are not enough to understand the changes. You are encouraged to pull down the code locally to better evaluate the changes:
```
git fetch origin pull/<PR ID>/head:<BRANCHNAME>
git checkout <BRANCHNAME>
```

### Run tests
After pulling down the code, you should try running the tests locally to ensure you understand the changes and that they work as expected.

## Approving Pull Requests

### Code Maintainers
Solo.io has a dedicated team maintaining the Gloo Gateway project. When you submit a PR for Gloo Gateway, one of these maintainers will initiate the automation on your PR, and provide a review.

If there are particular areas of the codebase that are being modified, it is important to identify subject-matter experts (SME) for those changes. If you do not know the SME, use the `@solo-io/gloo-edge-leads` GitHub handle to tag the Gloo Gateway technical leads, and they will identify the SMEs. If you can explicitly identify the domains that are being affected, it is easier to identify the relevant parties to review the code.

### Requirements
Each PR requires 2 maintainers to approve the changes. The intention here is to distribute knowledge across more people, and provide an opportunity for broader perspective when reviewing the changes.

### Shared Ownership
By approving a pull request, you are indicating that you have reviewed the changes and are confident that they are correct and to your understanding will not cause any issues. You are signing on as a co-author of the changes, and we expect that if you are comfortable approving a PR, you are also comfortable with this responsibility, which includes being a point of contact regarding these changes in the future.

*If the changes look good to you, but you recognize that you are not the best person to review the changes, instead of approving the PR, comment that the changes look good to you and clarify why you aren't comfortable approving.*

### Open Conversations
It it up to the author and reviewers to make sure all conversations are resolved, though we recommend the following strategy:
- If there is a trivial technical proposal that the PR author implemented, they can resolve the conversation when it's completed
- If there is a nit, the PR author has complete autonomy to decide how to proceed
- If there is a non-trivial technical proposal that the PR author implemented, they should leave the conversation open to ensure the reviewer is satisfied with the change

**An important side-effect of this, is that as a reviewer if you only have nits on a PR, you should approve that PR**

