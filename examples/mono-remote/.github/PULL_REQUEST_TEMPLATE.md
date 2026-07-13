<!-- DO NOT MERGE — this is an attic overlay store, not a collaborative repo. -->

# ⛔ DO NOT MERGE — attic overlay store

Every branch here is `host/<fingerprint>`: an **independent orphan history** rooted
at a different repo. Merging one branch into another splices unrelated trees and
corrupts a repo's overlay. There is no valid pull request on this remote.

Push and sync overlays with attic instead:

```sh
attic push        # publish this repo's overlay
attic sync        # fetch + integrate + push, same branch
```

Any PR opened here is closed automatically. If you reached this template, close the PR.
</content>
