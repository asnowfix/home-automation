package sfr

func GetHostsList() (*[]*LanHost, error) {
	if len(token) == 0 {
		renewToken()
	}
	params := map[string]string{
		"token": token,
	}
	res, err := queryBox("lan.getHostsList", &params)
	if err != nil {
		log.Info("lan.getHostsList", err)
		return nil, err
	}

	return res.(*[]*LanHost), nil
}

func GetDnsHostList() (*[]*DnsHost, error) {
	if len(token) == 0 {
		renewToken()
	}
	params := map[string]string{
		"token": token,
	}
	res, err := queryBox("lan.getDnsHostList", &params)
	if err != nil {
		log.Info("lan.getDnsHostList", err)
		return nil, err
	}

	return res.(*[]*DnsHost), nil
}
