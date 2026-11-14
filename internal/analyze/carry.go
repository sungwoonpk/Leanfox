package analyze

import (
	"math"
	"sort"
)

// groups are the kinds of text whose density is measured separately, because code, prose and tool output do not
// tokenize alike.
var groups = []string{GroupUserText, GroupAttachments, GroupClaudeText, GroupThinking, GroupToolCalls, GroupToolResults}

func groupIndex(g string) int {
	for i, n := range groups {
		if n == g {
			return i
		}
	}
	return -1
}

// carry splits the input-side cost of the main thread among the kinds of text that were carried.
//
// Within a segment, the context of request k is the fixed base plus everything added since, so
//
//	context[k] - context[0] = sum over kinds of (rate of the kind * amount of that kind added up to request k)
//
// where the amount is characters, except for reasoning, which the file keeps almost none of and which is counted in
// blocks. The rates, in tokens per unit, are the non-negative numbers that make this true as closely as possible over all
// requests of all segments (least squares, which weights the late requests where the context is large and the money
// is). They are measured, not assumed. A kind of text that the model never receives again, such as reasoning that is
// dropped from the history, comes out at zero. The median error of rebuilding each request's context with these rates
// is reported as FitError, and it says how far the split can be trusted.
func carry(a *Analysis, segs []*segment, u unit, opts Options) {
	rates, fitErr := fitRates(segs)
	c := &a.Carry
	c.FitError = fitErr
	for i, g := range groups {
		unit := "character"
		if g == GroupThinking {
			unit = "block"
		}
		c.Rates = append(c.Rates, GroupRate{Group: g, Unit: unit, Tokens: rates[i]})
	}

	slices := map[[2]string]*Slice{}
	var carriers []*item
	for _, sg := range segs {
		n := len(sg.ctx)
		base := slice(slices, GroupBase, "system prompt, tools, memory and the first message")
		base.Items++
		base.Tokens += sg.ctx[0]
		// The base is written once if the first request had to write it, and only read if the cache was already warm,
		// which is the case when an earlier session left the system prompt in it. The first request says which.
		written := float64(min(sg.cw[0], sg.ctx[0]))
		base.CarryUSD += written*u.write + (float64(sg.ctx[0])-written)*u.read + float64(sg.ctx[0])*float64(n-1)*u.read

		for i := range sg.items {
			it := &sg.items[i]
			if it.at < 1 {
				continue // already part of the base
			}
			it.tokens = rates[groupIndex(it.group)] * it.units
			it.carried = n - it.at
			it.carry = it.tokens * (u.write + float64(it.carried-1)*u.read)
			sl := slice(slices, it.group, it.name)
			sl.Items++
			sl.Tokens += int64(math.Round(it.tokens))
			sl.CarryUSD += it.carry
			if it.group == GroupToolResults {
				carriers = append(carriers, it)
			}
		}
	}

	var explained float64
	for _, sl := range slices {
		if sl.CarryUSD == 0 && sl.Tokens == 0 && sl.Group != GroupBase {
			continue // a kind that is never carried has nothing to show
		}
		explained += sl.CarryUSD
		if c.InputSideUSD > 0 {
			sl.Share = sl.CarryUSD / c.InputSideUSD
		}
		c.Slices = append(c.Slices, *sl)
	}
	sort.Slice(c.Slices, func(i, j int) bool {
		if c.Slices[i].CarryUSD != c.Slices[j].CarryUSD {
			return c.Slices[i].CarryUSD > c.Slices[j].CarryUSD
		}
		return c.Slices[i].Name < c.Slices[j].Name
	})
	for _, sg := range segs {
		for k := 1; k < len(sg.ctx); k++ {
			grown := max(0, sg.ctx[k]-sg.ctx[k-1])
			if extra := sg.cw[k] - grown; extra >= minRebuildTokens {
				c.Rebuilds++
				c.RebuildUSD += float64(extra) * (u.write - u.read)
			}
		}
	}
	c.Unexplained = c.InputSideUSD - explained - c.RebuildUSD

	sort.SliceStable(carriers, func(i, j int) bool { return carriers[i].carry > carriers[j].carry })
	for i, it := range carriers {
		if i == opts.TopCarriers {
			break
		}
		cr := Carrier{
			Tool: it.tool, What: it.what, Tokens: int64(math.Round(it.tokens)), Carried: it.carried, CarryUSD: it.carry,
		}
		if c.InputSideUSD > 0 {
			cr.Share = it.carry / c.InputSideUSD
		}
		a.Carriers = append(a.Carriers, cr)
	}
}

// A request that wrote this many more tokens than the conversation grew by is counted as a cache rebuild.
const minRebuildTokens = 2000

func slice(m map[[2]string]*Slice, group, name string) *Slice {
	k := [2]string{group, name}
	if m[k] == nil {
		m[k] = &Slice{Group: group, Name: name}
	}
	return m[k]
}

// priors are weak starting values for the rates, used only when a session is too short to measure them. Each has the
// weight of a few requests of typical size, so a long session overrules them completely. They are the densities seen
// on the sessions this was built on: prose near 0.4 tokens per character, code and tool output near 0.5, and about a
// thousand tokens for a block of reasoning.
var priors = map[string]struct{ rate, weight float64 }{
	GroupUserText:    {0.4, 1e8},
	GroupAttachments: {0.1, 1e8},
	GroupClaudeText:  {0.35, 1e8},
	GroupThinking:    {950, 125},
	GroupToolCalls:   {0.5, 1e8},
	GroupToolResults: {0.45, 1e8},
}

// fitRates finds the tokens per unit of each group, and the median relative error of the fit.
func fitRates(segs []*segment) (rates []float64, medianErr float64) {
	g := len(groups)
	var cols [][]float64
	for range groups {
		cols = append(cols, nil)
	}
	var y, ctx []float64
	for _, sg := range segs {
		n := len(sg.ctx)
		added := make([][]float64, g)
		for i := range added {
			added[i] = make([]float64, n)
		}
		for _, it := range sg.items {
			if it.at >= 1 {
				added[groupIndex(it.group)][it.at] += it.units
			}
		}
		cum := make([]float64, g)
		for k := 1; k < n; k++ {
			for i := 0; i < g; i++ {
				cum[i] += added[i][k]
				cols[i] = append(cols[i], cum[i])
			}
			y = append(y, float64(sg.ctx[k]-sg.ctx[0]))
			ctx = append(ctx, float64(sg.ctx[k]))
		}
	}
	// The priors enter as one extra row per group, standing in for data: sqrt(weight) on that group's column and the
	// matching value on the right. The error below is measured on the real rows only.
	real := len(y)
	for j, name := range groups {
		p := priors[name]
		for i := range cols {
			v := 0.0
			if i == j {
				v = math.Sqrt(p.weight)
			}
			cols[i] = append(cols[i], v)
		}
		y = append(y, math.Sqrt(p.weight)*p.rate)
	}
	rates = nnls(cols, y)
	errs := make([]float64, real)
	for i := 0; i < real; i++ {
		var pred float64
		for j := 0; j < g; j++ {
			pred += rates[j] * cols[j][i]
		}
		errs[i] = math.Abs(pred-y[i]) / ctx[i]
	}
	return rates, median(errs)
}

// nnls solves least squares with every coefficient at least zero (Lawson and Hanson's active set method), on the normal
// equations of columns scaled to unit length. The kinds of text grow together over a session, so the columns are close
// to each other, and a method that only improves one coefficient at a time stops long before the answer. Here the
// system is a handful of columns, and the answer is exact when the data is.
func nnls(cols [][]float64, y []float64) []float64 {
	g := len(cols)
	out := make([]float64, g)
	if len(y) == 0 {
		return out
	}
	norm := make([]float64, g)
	for j := range cols {
		for _, v := range cols[j] {
			norm[j] += v * v
		}
		norm[j] = math.Sqrt(norm[j])
	}
	gram := make([][]float64, g)
	for i := range gram {
		gram[i] = make([]float64, g)
	}
	rhs := make([]float64, g)
	for i := 0; i < g; i++ {
		if norm[i] == 0 {
			continue
		}
		for k, v := range cols[i] {
			rhs[i] += v / norm[i] * y[k]
		}
		for j := i; j < g; j++ {
			if norm[j] == 0 {
				continue
			}
			var sum float64
			for k, v := range cols[i] {
				sum += v * cols[j][k]
			}
			gram[i][j] = sum / (norm[i] * norm[j])
			gram[j][i] = gram[i][j]
		}
	}

	const tol = 1e-10
	x := make([]float64, g)
	passive := make([]bool, g)
	dead := make([]bool, g) // a column that is zero, or that cannot be told apart from the ones already in
	for j := range dead {
		dead[j] = norm[j] == 0
	}
	for range 10 * g {
		best, enter := tol, -1
		for j := 0; j < g; j++ {
			if passive[j] || dead[j] {
				continue
			}
			w := rhs[j]
			for i := range x {
				w -= gram[j][i] * x[i]
			}
			if w > best {
				best, enter = w, j
			}
		}
		if enter < 0 {
			break
		}
		passive[enter] = true
		for {
			z, ok := solveSubset(gram, rhs, passive)
			if !ok {
				// A column that is, to within rounding, a combination of the ones already in. Floating point makes it
				// hard to build such a case on purpose, so this guard against dividing by zero is not under a test.
				passive[enter], dead[enter] = false, true
				break
			}
			positive := true
			for j := range z {
				if passive[j] && z[j] <= tol {
					positive = false
				}
			}
			if positive {
				copy(x, z)
				break
			}
			alpha := 1.0
			for j := range z {
				if passive[j] && z[j] <= tol {
					alpha = math.Min(alpha, x[j]/(x[j]-z[j]))
				}
			}
			for j := range x {
				x[j] += alpha * (z[j] - x[j])
			}
			for j := range x {
				if passive[j] && x[j] <= tol {
					passive[j], x[j] = false, 0
				}
			}
		}
	}
	for j := range x {
		if norm[j] > 0 {
			out[j] = x[j] / norm[j]
		}
	}
	return out
}

// solveSubset solves the normal equations restricted to the chosen columns by Gaussian elimination with pivoting. It
// reports false when they are singular, which means one of them is a combination of the others.
func solveSubset(gram [][]float64, rhs []float64, in []bool) ([]float64, bool) {
	var idx []int
	for j, ok := range in {
		if ok {
			idx = append(idx, j)
		}
	}
	n := len(idx)
	a := make([][]float64, n)
	for r := range a {
		a[r] = make([]float64, n+1)
		for c := range idx {
			a[r][c] = gram[idx[r]][idx[c]]
		}
		a[r][n] = rhs[idx[r]]
	}
	for col := 0; col < n; col++ {
		piv := col
		for r := col + 1; r < n; r++ {
			if math.Abs(a[r][col]) > math.Abs(a[piv][col]) {
				piv = r
			}
		}
		if math.Abs(a[piv][col]) < 1e-9 {
			return nil, false
		}
		a[col], a[piv] = a[piv], a[col]
		for r := col + 1; r < n; r++ {
			f := a[r][col] / a[col][col]
			for c := col; c <= n; c++ {
				a[r][c] -= f * a[col][c]
			}
		}
	}
	z := make([]float64, len(in))
	for r := n - 1; r >= 0; r-- {
		v := a[r][n]
		for c := r + 1; c < n; c++ {
			v -= a[r][c] * z[idx[c]]
		}
		z[idx[r]] = v / a[r][r]
	}
	return z, true
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// repeats totals the text that came back although it was already in the conversation.
func repeats(a *Analysis, segs []*segment) {
	rows := map[string]*RepeatRow{}
	for _, sg := range segs {
		for i := range sg.items {
			it := &sg.items[i]
			if it.at < 1 || (!it.reread && !it.same && !it.rewrite) {
				continue
			}
			tot := &a.Repeats.SameOutputs
			switch {
			case it.rewrite:
				tot = &a.Repeats.Rewrites
			case it.reread:
				tot = &a.Repeats.Rereads
			}
			tok := int64(math.Round(it.tokens))
			tot.Count++
			tot.Tokens += tok
			tot.USD += it.carry
			row := rows[it.what]
			if row == nil {
				row = &RepeatRow{What: it.what}
				rows[it.what] = row
			}
			row.Times++
			row.Tokens += tok
			row.USD += it.carry
		}
	}
	for _, r := range rows {
		a.Repeats.Top = append(a.Repeats.Top, *r)
	}
	// By name first, then by cost with a stable sort, so that equal costs come out in a fixed order.
	sort.Slice(a.Repeats.Top, func(i, j int) bool { return a.Repeats.Top[i].What < a.Repeats.Top[j].What })
	sort.SliceStable(a.Repeats.Top, func(i, j int) bool { return a.Repeats.Top[i].USD > a.Repeats.Top[j].USD })
	if len(a.Repeats.Top) > 5 {
		a.Repeats.Top = a.Repeats.Top[:5]
	}
}
