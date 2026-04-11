# Limitations

This document records known limitations of the current Go implementation and
the practical guidance for using it safely.

## File Imports And Media Resolution

Tracked page imports and media imports work for normal MediaWiki pages and file
pages, but there are still cases where a file can exist on the wiki and the
helper still cannot retrieve the expected file payload.

This is most likely when:

- a historical file revision no longer resolves cleanly through `imageinfo`
- MediaWiki returns a file page but no usable download URL
- the file page exists while the underlying file revision is deleted,
  inaccessible, or otherwise unavailable

The helper already normalizes common title differences such as `_` versus
spaces, so these failures are no longer believed to be primarily caused by
simple title formatting differences.

Current behavior:

- file download failures during import are reported as warnings
- page import continues when possible
- a missing or inaccessible file revision may still be absent from the clone

Recommended usage:

- treat imported media as best-effort when cloning wikis with older or unusual
  file histories
- if exact media preservation is required, verify imported files separately

Related upstream issues:

- [#92](https://github.com/Git-Mediawiki/Git-Mediawiki/issues/92)

## Windows Checkout Of Namespaced Pages

Repository filenames are derived from MediaWiki titles. Namespace separators
such as `File:` and `Category:` are currently preserved in the repository
pathname.

That is acceptable on Unix-like systems, but it is not compatible with normal
Windows working-tree checkout because `:` is not a valid path character there.

Current behavior:

- bare repositories and server-side refs can still exist
- a normal checkout on Windows can fail for namespaced pages
- file pages are especially affected because they naturally use `File:...`

Recommended usage:

- do not rely on a regular Windows checkout for repositories that contain
  namespaced pages
- if Windows support becomes a goal, pathname encoding for `:` and the reverse
  mapping back to MediaWiki titles will need to be designed explicitly

Related upstream issues:

- [#41](https://github.com/Git-Mediawiki/Git-Mediawiki/issues/41)

## MediaWiki Moves Versus Git Renames

MediaWiki page moves and Git renames do not mean the same thing.

A Git rename is only a pathname change inside the repository. A MediaWiki move
can also:

- create a new revision at the destination title
- leave a redirect behind at the source title
- overwrite an existing redirect at the destination

Because of that, the helper does not currently try to convert Git rename
detection into a first-class MediaWiki move operation. It instead treats the
result as ordinary page edits and deletions.

Current behavior:

- revision import follows the page content returned by MediaWiki
- push uses file additions, edits, and deletions, not a dedicated move API
- a rename in Git should not be assumed to preserve MediaWiki move semantics

Recommended usage:

- do not rely on `git mv` to mean "perform a MediaWiki move"
- if redirect behavior or move logs matter, perform the move in MediaWiki and
  then import the resulting history

Related upstream issues:

- [#38](https://github.com/Git-Mediawiki/Git-Mediawiki/issues/38)
- [#2](https://github.com/Git-Mediawiki/Git-Mediawiki/issues/2)
