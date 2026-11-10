// SPDX-License-Identifier: MPL-2.0

package xml

// namespaces holds the declarations in scope in declaration order. Each entry
// records the earlier entry with the same prefix it shadows, so the effective
// scope is one pass over the list.
//
// An indexed set also maps each prefix to its visible entry and keeps, for each URI,
// a heap of exactly the visible entries with a non-empty prefix bound to it. An entry
// joins its heap when it becomes visible and leaves when it is shadowed or goes out of
// scope, so the indexes never hold more than the bindings in scope.
type namespaces struct {
	top    map[string]int
	byURI  map[string]*entryHeap
	list   []Binding
	shadow []int
	hidden []bool
	slot   []int
}

// indexed returns a set that maintains the prefix and URI indexes.
func indexed() namespaces {
	return namespaces{top: make(map[string]int), byURI: make(map[string]*entryHeap)}
}

func (n *namespaces) len() int { return len(n.list) }

func (n *namespaces) push(b Binding) {
	prev := -1
	i := len(n.list)
	if n.top != nil {
		if p, ok := n.top[b.Prefix]; ok {
			prev = p
		}
		n.top[b.Prefix] = i
	} else {
		for j := i - 1; j >= 0; j-- {
			if n.list[j].Prefix == b.Prefix {
				prev = j
				break
			}
		}
	}
	if prev >= 0 {
		n.hidden[prev] = true
	}
	n.list = append(n.list, b)
	n.shadow = append(n.shadow, prev)
	n.hidden = append(n.hidden, false)
	if n.top != nil {
		n.slot = append(n.slot, -1)
		if prev >= 0 {
			n.leave(prev)
		}
		n.join(i)
	}
}

func (n *namespaces) truncate(k int) {
	for i := len(n.list) - 1; i >= k; i-- {
		p := n.shadow[i]
		if p >= 0 {
			n.hidden[p] = false
		}
		if n.top != nil {
			n.leave(i)
			if p >= 0 {
				n.top[n.list[i].Prefix] = p
				n.join(p)
			} else {
				delete(n.top, n.list[i].Prefix)
			}
		}
	}
	clear(n.list[k:])
	n.list = n.list[:k]
	n.shadow = n.shadow[:k]
	n.hidden = n.hidden[:k]
	if n.top != nil {
		n.slot = n.slot[:k]
	}
}

// join adds visible entry i to the heap of its URI.
func (n *namespaces) join(i int) {
	if n.list[i].Prefix == "" {
		return
	}
	uri := n.list[i].URI
	h := n.byURI[uri]
	if h == nil {
		h = &entryHeap{}
		n.byURI[uri] = h
	}
	h.add(n, i)
}

// leave removes entry i from the heap of its URI when it is there.
func (n *namespaces) leave(i int) {
	if n.slot[i] < 0 {
		return
	}
	uri := n.list[i].URI
	h := n.byURI[uri]
	h.remove(n, i)
	if len(h.items) == 0 {
		delete(n.byURI, uri)
	}
}

func (n *namespaces) lookup(prefix string) (string, bool) {
	if prefix == "xml" {
		return XMLNamespace, true
	}
	if len(n.list) == 0 {
		return "", prefix == ""
	}
	if n.top != nil {
		if i, ok := n.top[prefix]; ok {
			return n.list[i].URI, true
		}
		return "", prefix == ""
	}
	for i := len(n.list) - 1; i >= 0; i-- {
		if n.list[i].Prefix == prefix {
			return n.list[i].URI, true
		}
	}
	return "", prefix == ""
}

// declared reports whether prefix is declared at or after index from.
func (n *namespaces) declared(prefix string, from int) bool {
	for _, b := range n.list[from:] {
		if b.Prefix == prefix {
			return true
		}
	}
	return false
}

// visible appends the effective bindings, excluding the implicit xml prefix.
func (n *namespaces) visible(dst []Binding) []Binding {
	for i, b := range n.list {
		if !n.hidden[i] && b.Prefix != "xml" {
			dst = append(dst, b)
		}
	}
	return dst
}

func (n *namespaces) reset() {
	n.top = nil
	n.byURI = nil
	n.list = nil
	n.shadow = nil
	n.hidden = nil
	n.slot = nil
}

// rebind changes the URI of entry i of an indexed set.
func (n *namespaces) rebind(i int, uri string) {
	visible := n.slot[i] >= 0
	if visible {
		n.leave(i)
	}
	n.list[i].URI = uri
	if visible || !n.hidden[i] {
		n.join(i)
	}
}

// visibleAt returns the index of the visible entry for prefix in an indexed set.
func (n *namespaces) visibleAt(prefix string) (int, bool) {
	i, ok := n.top[prefix]
	return i, ok
}

// smallestPrefix returns the smallest non-empty visible prefix bound to uri.
func (n *namespaces) smallestPrefix(uri string) string {
	if h := n.byURI[uri]; h != nil {
		return n.list[h.items[0]].Prefix
	}
	return ""
}

// entryHeap is a min-heap of entry indexes ordered by prefix; namespaces.slot records
// each entry's position so it can be removed in logarithmic time.
type entryHeap struct {
	items []int
}

func (h *entryHeap) less(n *namespaces, a, b int) bool {
	return n.list[h.items[a]].Prefix < n.list[h.items[b]].Prefix
}

func (h *entryHeap) swap(n *namespaces, a, b int) {
	h.items[a], h.items[b] = h.items[b], h.items[a]
	n.slot[h.items[a]] = a
	n.slot[h.items[b]] = b
}

func (h *entryHeap) add(n *namespaces, i int) {
	h.items = append(h.items, i)
	n.slot[i] = len(h.items) - 1
	h.up(n, len(h.items)-1)
}

func (h *entryHeap) remove(n *namespaces, i int) {
	at := n.slot[i]
	last := len(h.items) - 1
	if at != last {
		h.swap(n, at, last)
	}
	h.items = h.items[:last]
	n.slot[i] = -1
	if at < last {
		h.down(n, at)
		h.up(n, at)
	}
}

func (h *entryHeap) up(n *namespaces, at int) {
	for at > 0 {
		parent := (at - 1) / 2
		if !h.less(n, at, parent) {
			return
		}
		h.swap(n, at, parent)
		at = parent
	}
}

func (h *entryHeap) down(n *namespaces, at int) {
	for {
		least, l, r := at, 2*at+1, 2*at+2
		if l < len(h.items) && h.less(n, l, least) {
			least = l
		}
		if r < len(h.items) && h.less(n, r, least) {
			least = r
		}
		if least == at {
			return
		}
		h.swap(n, at, least)
		at = least
	}
}
