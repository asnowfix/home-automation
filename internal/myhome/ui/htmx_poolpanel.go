package ui

import (
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

// PoolPanel renders the Pool section from pool.getstatus, the same source the
// device card's pool tags use. When the verb is not registered (pool tracking
// disabled) it renders an empty state; it never invents values.
func (h *HTMXHandler) PoolPanel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := poolPanelView{State: "disabled"}
	if mh, err := myhome.Methods(myhome.PoolGetStatus); err == nil {
		view.State = "unavailable"
		if res, err := mh.ActionE(h.ctx, nil); err == nil {
			if st, ok := res.(*myhome.PoolGetStatusResult); ok {
				view = poolPanelView{
					State:             "ok",
					Name:              st.DeviceID,
					WaterSupplyActive: st.WaterSupplyActive,
					TurnoverAchieved:  st.TurnoverAchieved,
					TurnoverTarget:    st.TurnoverTarget,
					RuntimeMin:        st.RuntimeSec / 60,
				}
				if d, err := h.db.GetDeviceById(h.ctx, st.DeviceID); err == nil && d != nil && d.Name() != "" {
					view.Name = d.Name()
				}
			}
		}
	}
	if err := poolPanelTmpl.Execute(w, view); err != nil {
		h.log.Error(err, "failed to render pool panel")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
