package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
    "regexp"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type RewritesLabel struct {
	Domain string `json:"domain"`
	IP     string `json:"answer"`
}

type ContainerState map[string][]RewritesLabel


func parseRewritesLabel(labelContent string) []RewritesLabel {
    rewrites := []RewritesLabel{}
    regexPattern := `Rewrite\(([^,]+),([^)]+)\)`
    re := regexp.MustCompile(regexPattern)
    matches := re.FindAllStringSubmatch(labelContent, -1)
    if len(matches) == 0 {
        return rewrites
    }
    for _, match := range matches {
        if len(match) == 3 {
            domain := strings.TrimSpace(match[1])
            ip := strings.TrimSpace(match[2])
            if len(domain) > 0 && (domain[0] == '"' || domain[0] == '\'') {
                domain = domain[1 : len(domain)-1]
            }
            if len(ip) > 0 && (ip[0] == '"' || ip[0] == '\'') {
                ip = ip[1 : len(ip)-1]
            }
            rewrites = append(rewrites, RewritesLabel{Domain: domain, IP: ip})
        }
    }
    return rewrites
}

func createAuthHeader(username, password string) string {
	credentials := fmt.Sprintf("%s:%s", username, password)
	return base64.StdEncoding.EncodeToString([]byte(credentials))
}

func sendAPICall(method, url, credentials string, requestBody []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Basic "+credentials)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}

	return resp.StatusCode, respBody, nil
}

func saveState(filename string, state ContainerState) error {
	jsonContent, err := json.Marshal(state)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filename, jsonContent, 0644)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	fmt.Println("Starting Docker DNS Sync Application...")

	adguardURL := os.Getenv("AdguardURL")
	adguardUser := os.Getenv("AdguardUser")
	adguardPassword := os.Getenv("AdguardPassword")
	stateFilename := "/data/state.json"

	if adguardURL == "" || adguardUser == "" || adguardPassword == "" {
		fmt.Println("Error: AdguardURL, AdguardUser, or AdguardPassword environment variables are missing.")
		return
	}

	if _, err := os.Stat(stateFilename); os.IsNotExist(err) {
		err := ioutil.WriteFile(stateFilename, []byte("{}"), 0644)
		if err != nil {
			fmt.Println("Error creating state file:", err)
			return
		}
	}

	credentials := createAuthHeader(adguardUser, adguardPassword)

    httpClient := &http.Client{}
    req, err := http.NewRequest("GET", adguardURL+"/control/rewrite/list", nil)
    if err != nil {
        fmt.Println("Error creating HTTP request:", err)
        return
    }
    req.Header.Set("Authorization", "Basic "+credentials)

    resp, err := httpClient.Do(req)
    if err != nil {
        fmt.Println("Error retrieving domains from Adguard API:", err)
        return
    }
    defer resp.Body.Close()

    respBody, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        fmt.Println("Error reading response body:", err)
        return
    }

    var domains []RewritesLabel
    err = json.Unmarshal(respBody, &domains)
    if err != nil {
        fmt.Println("Error decoding JSON response:", err)
        fmt.Println("Response body:", string(respBody))
        return
    }

    fmt.Println("Domains in Adguard API:")
    for _, domain := range domains {
        fmt.Printf("Domain: %s, IP: %s\n", domain.Domain, domain.IP)
    }

	fmt.Println("Checking running Docker containers...")

	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Println("Error creating Docker client:", err)
		return
	}

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		fmt.Println("Error listing Docker containers:", err)
		return
	}

	state := make(ContainerState)


	for _, container := range containers {
		fmt.Println("Checking container:", container.ID)

		rewrites, exists := container.Labels["syncdns.rewrites"]
		if exists {
			fmt.Println("Found syncdns.rewrites label in container:", container.ID)

			labels := parseRewritesLabel(rewrites)

			for _, label := range labels {
				url := fmt.Sprintf("%s/control/rewrite/add", adguardURL)
				requestBody, _ := json.Marshal(map[string]string{
					"domain": label.Domain,
					"answer": label.IP,
				})

				if !domainExistsInState(container.ID, label.Domain, label.IP, state) && !domainExistsInAPIList(label.Domain, label.IP, adguardURL, credentials) {
					fmt.Printf("Adding domain %s with IP %s to Adguard\n", label.Domain, label.IP)

					statusCode, _, err := sendAPICall("POST", url, credentials, requestBody)
					if err != nil {
						fmt.Println("Error sending HTTP request:", err)
						continue
					}

					if statusCode == http.StatusOK {
						fmt.Println("Domain", label.Domain, "with IP", label.IP, "added successfully")
						state[container.ID] = append(state[container.ID], label)
						err := saveState(stateFilename, state)
						if err != nil {
							fmt.Println("Error saving state:", err)
						}
					} else {
						fmt.Println("Error adding domain", label.Domain, "with IP", label.IP, "Status code:", statusCode)
					}
				} else {
					fmt.Printf("Domain %s with IP %s already exists in the state or Adguard\n", label.Domain, label.IP)
				}
			}
		}
	}

	fmt.Println("Watching Docker events...")

	events, errs := cli.Events(ctx, types.EventsOptions{})

	for {
		select {
		case event := <-events:
			if event.Action == "start" || event.Action == "unpause" {
				inspect, err := cli.ContainerInspect(ctx, event.ID)
				if err != nil {
					fmt.Println("Error inspecting container:", err)
					continue
				}

				rewrites, exists := inspect.Config.Labels["syncdns.rewrites"]
				if exists {
					fmt.Println("Found syncdns.rewrites label in started container:", event.ID)

					labels := parseRewritesLabel(rewrites)

					for _, label := range labels {
						url := fmt.Sprintf("%s/control/rewrite/add", adguardURL)
						requestBody, _ := json.Marshal(map[string]string{
							"domain": label.Domain,
							"answer": label.IP,
						})

						if !domainExistsInState(event.ID, label.Domain, label.IP, state) && !domainExistsInAPIList(label.Domain, label.IP, adguardURL, credentials) {
							fmt.Printf("Adding domain %s with IP %s to Adguard\n", label.Domain, label.IP)

							statusCode, _, err := sendAPICall("POST", url, credentials, requestBody)
							if err != nil {
								fmt.Println("Error sending HTTP request:", err)
								continue
							}

							if statusCode == http.StatusOK {
								fmt.Println("Domain", label.Domain, "with IP", label.IP, "added successfully")
								state[event.ID] = append(state[event.ID], label)
								err := saveState(stateFilename, state)
								if err != nil {
									fmt.Println("Error saving state:", err)
								}
							} else {
								fmt.Println("Error adding domain", label.Domain, "with IP", label.IP, "Status code:", statusCode)
							}
						} else {
							fmt.Printf("Domain %s with IP %s already exists in the state or Adguard\n", label.Domain, label.IP)
						}
					}
				}
			} else if event.Action == "stop" || event.Action == "kill" || event.Action == "die" || event.Action == "pause" || event.Action == "restart"  {
                labels, exists := state[event.ID]
                if exists {
                    for _, label := range labels {
                        if domainExistsInAPIList(label.Domain, label.IP, adguardURL, credentials) {
                            url := fmt.Sprintf("%s/control/rewrite/delete", adguardURL)
                            requestBody, _ := json.Marshal(map[string]string{
                                "domain": label.Domain,
                                "answer": label.IP,
                            })

                            statusCode, _, err := sendAPICall("POST", url, credentials, requestBody)
                            if err != nil {
                                fmt.Println("Error sending HTTP request:", err)
                                continue
                            }

                            if statusCode == http.StatusOK {
                                fmt.Println("Domain", label.Domain, "with IP", label.IP, "removed successfully")
                            } else {
                                fmt.Println("Error removing domain", label.Domain, "with IP", label.IP, "Status code:", statusCode)
                            }
                        } else {
                            fmt.Println("Domain", label.Domain, "with IP", label.IP, "is not in Adguard")
                        }
                    }


                    delete(state, event.ID)
                    err := saveState(stateFilename, state)
                    if err != nil {
                        fmt.Println("Error saving state:", err)
                    }
                }
			}
		case err := <-errs:
			fmt.Println("Error receiving Docker events:", err)
		}
	}
}

func domainExistsInState(containerID, domain, ip string, state ContainerState) bool {
	labels, exists := state[containerID]
	if !exists {
		return false
	}

	for _, label := range labels {
		if label.Domain == domain && label.IP == ip {
			return true
		}
	}
	return false
}

func domainExistsInAPIList(domain, ip, adguardURL, credentials string) (bool) {
	httpClient := &http.Client{}
    req, err := http.NewRequest("GET", adguardURL+"/control/rewrite/list", nil)
    if err != nil {
        fmt.Println("Error creating HTTP request:", err)
        return false
    }
    req.Header.Set("Authorization", "Basic "+credentials)
    resp, err := httpClient.Do(req)

	defer resp.Body.Close()

	var existingDomains []RewritesLabel
	err = json.NewDecoder(resp.Body).Decode(&existingDomains)
	if err != nil {
		return false
	}

	for _, d := range existingDomains {
		if d.Domain == domain && d.IP == ip {
			return true
		}
	}
	return false
}