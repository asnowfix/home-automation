package sfr

import "log"

func LanGetHostsList() ([]*XmlHost, error) {
	if len(token) == 0 {
		renewToken()
	}
	params := map[string]string{
		"token": token,
	}
	res, err := queryBox("lan.getHostsList", &params)
	if err != nil {
		log.Default().Println(err)
		return nil, err
	}

	return res.([]*XmlHost), nil
}
