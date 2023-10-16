{{range $vrf := .}}
{{if $vrf.ShouldTemplateVRF}}
  neighbor dv.{{$vrf.Name}} activate
  neighbor dv.{{$vrf.Name}} allowas-in origin
  neighbor dv.{{$vrf.Name}} soft-reconfiguration inbound
  neighbor dv.{{$vrf.Name}} route-map rm_{{$vrf.Name}}_import in
  neighbor dv.{{$vrf.Name}} route-map rm_{{$vrf.Name}}_export out
{{- else }}
{{range $item := $vrf.AggregateIPv4}}
  aggregate-address {{$item}}
{{- end }}
{{end}}
{{- end -}}