package ui

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/asnowfix/home-automation/internal/myhome"
)

// poolPanelView is the data for the Pool section fragment. State is one of
// "disabled" (pool tracking not registered), "unavailable" (the status call
// failed) or "ok".
type poolPanelView struct {
	State             string
	Name              string
	WaterSupplyActive bool
	TurnoverAchieved  float64
	TurnoverTarget    float64
	RuntimeMin        int64
}

const poolPanelTemplate = `{{if eq .State "disabled"}}
<p class="has-text-grey">Pool tracking is not enabled.</p>
{{else if eq .State "unavailable"}}
<p class="has-text-grey">Pool status is unavailable right now.</p>
{{else}}
<div class="mh-row" id="pool-row">
  <p class="mh-row-name">{{.Name}}</p>
  <p class="mh-row-pills">
    {{if .WaterSupplyActive}}<span class="tag is-warning">Water supply: pump paused</span>
    {{else}}<span class="tag is-success">Water supply: OK</span>{{end}}
    <span class="tag">Filtered today: {{printf "%.1f" .TurnoverAchieved}} / {{printf "%.1f" .TurnoverTarget}} x</span>
    <span class="tag">Pump runtime today: {{.RuntimeMin}} min</span>
  </p>
</div>
{{end}}`

var poolPanelTmpl = template.Must(template.New("pool-panel").Parse(poolPanelTemplate))

// poolViewFromStatus maps a pool.getstatus outcome to the fragment state:
// ErrPoolNotEnabled is "disabled" (a calm empty state), any other error is
// "unavailable".
func poolViewFromStatus(res any, err error) poolPanelView {
	if errors.Is(err, myhome.ErrPoolNotEnabled) {
		return poolPanelView{State: "disabled"}
	}
	if err != nil {
		return poolPanelView{State: "unavailable"}
	}
	st, ok := res.(*myhome.PoolGetStatusResult)
	if !ok {
		return poolPanelView{State: "unavailable"}
	}
	return poolPanelView{
		State:             "ok",
		Name:              st.DeviceID,
		WaterSupplyActive: st.WaterSupplyActive,
		TurnoverAchieved:  st.TurnoverAchieved,
		TurnoverTarget:    st.TurnoverTarget,
		RuntimeMin:        st.RuntimeSec / 60,
	}
}

// PoolPanel renders the Pool section from pool.getstatus, the same source the
// device card's pool tags use. It never invents values.
func (h *HTMXHandler) PoolPanel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := poolPanelView{State: "disabled"}
	if mh, err := myhome.Methods(myhome.PoolGetStatus); err == nil {
		res, err := mh.ActionE(r.Context(), nil)
		view = poolViewFromStatus(res, err)
		if view.State == "ok" {
			if d, err := h.db.GetDeviceById(r.Context(), view.Name); err == nil && d != nil && d.Name() != "" {
				view.Name = d.Name()
			}
		}
	}
	if err := poolPanelTmpl.Execute(w, view); err != nil {
		h.log.Error(err, "failed to render pool panel")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
