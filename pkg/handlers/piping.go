package handlers

import (
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/stuart-warren/pair/pkg/contextio"
)

type reqRes struct {
	req *http.Request
	res http.ResponseWriter
}

type reqResAndUnsubscribe struct {
	reqRes                   reqRes
	unsubscribeCloseListener func()
}

type pipe struct {
	sender    reqRes
	recievers []reqRes
}

type unestablishedPipe struct {
	sender     reqResAndUnsubscribe
	recievers  []reqResAndUnsubscribe
	nRecievers int
}

func (u *unestablishedPipe) getPipeIfEstablished() (pipe, error) {
	if len(u.recievers) != u.nRecievers {
		return pipe{}, fmt.Errorf("recievers don't match expected number of recievers")
	}
	var recievers []reqRes
	for _, r := range u.recievers {
		recievers = append(recievers, r.reqRes)
		r.unsubscribeCloseListener()
	}
	return pipe{
		sender:    u.sender.reqRes,
		recievers: recievers,
	}, nil
}

type pathToEstablished struct {
	paths map[string]interface{}
}

func (p *pathToEstablished) has(path string) bool {
	_, ok := p.paths[path]
	return ok
}

func (p *pathToEstablished) add(path string) bool {
	if p.has(path) {
		return false
	}
	p.paths[path] = nil
	return true
}

func (p *pathToEstablished) del(path string) {
	delete(p.paths, path)
}

func getNumberOfRecievers(r *http.Request) int {
	n := 1
	if param := r.URL.Query().Get("n"); param != "" {
		if paramInt, err := strconv.Atoi(param); err == nil && paramInt > 1 {
			n = paramInt
		}
	}
	return n
}

var blankReqResAndUnsubscribe reqResAndUnsubscribe = reqResAndUnsubscribe{}

// FIXME: complete these parts
func (s *server) handleReciever(w http.ResponseWriter, r *http.Request) {
	// // If the receiver requests Service Worker registration
	// // (from: https://speakerdeck.com/masatokinugawa/pwa-study-sw?slide=32)"
	if r.Header.Get("Service-Worker") == "script" {
		s.log.Printf("[ERROR] reciever attempting Service Worker registration: rejected")
		http.Error(w, "service worker registration denied", http.StatusBadRequest)
		return
	}
	// if (req.headers["service-worker"] === "script") {
	// 	// Reject Service Worker registration
	// 	res.writeHead(400, {
	// 	  "Access-Control-Allow-Origin": "*"
	// 	});
	// 	res.end(`[ERROR] Service Worker registration is rejected.\n`);
	// 	return;
	//   }
	nRecievers := getNumberOfRecievers(r)
	if nRecievers <= 0 {
		s.log.Printf("[ERROR] url parameter 'n' should be greater than 0, but recieved: %d", nRecievers)
		http.Error(w, "[ERROR] specify a positive number of recievers", http.StatusBadRequest)
		return
	}
	//   // Get the number of receivers
	//   const nReceivers = Server.getNReceivers(req.url);
	//   // If the number of receivers is invalid
	//   if (nReceivers <= 0) {
	// 	res.writeHead(400, {
	// 	  "Access-Control-Allow-Origin": "*"
	// 	});
	// 	res.end(`[ERROR] n should > 0, but n = ${nReceivers}.\n`);
	// 	return;
	//   }
	path := r.URL.Path
	if s.pathToEstablished.has(path) {
		http.Error(w, fmt.Sprintf("[ERROR] Connection on %s has been established already\n", path), http.StatusConflict)
		return
	}
	//   // The connection has been established already
	//   if (this.pathToEstablished.has(reqPath)) {
	// 	res.writeHead(400, {
	// 	  "Access-Control-Allow-Origin": "*"
	// 	});
	// 	res.end(`[ERROR] Connection on '${reqPath}' has been established already.\n`);
	// 	return;
	//   }
	unestablished, ok := s.pathToUnestablishedPipe[path]
	if !ok {
		reciever := s.createSenderOrReciever(recieverType, w, r)
		s.pathToUnestablishedPipe[path] = unestablishedPipe{
			recievers:  []reqResAndUnsubscribe{reciever},
			sender:     blankReqResAndUnsubscribe,
			nRecievers: nRecievers,
		}
	}
	if unestablished.nRecievers != nRecievers {
		http.Error(w, "[ERROR] the number of recievers is incorrect", http.StatusBadRequest)
		return
	}
	//   // Get unestablishedPipe
	//   const unestablishedPipe = this.pathToUnestablishedPipe.get(reqPath);
	//   // If the path connection is not connecting
	//   if (unestablishedPipe === undefined) {
	// 	// Create a receiver
	// 	/* tslint:disable:no-shadowed-variable */
	// 	const receiver = this.createSenderOrReceiver("receiver", req, res, reqPath);
	// 	// Set a receiver
	// 	this.pathToUnestablishedPipe.set(reqPath, {
	// 	  receivers: [receiver],
	// 	  nReceivers: nReceivers
	// 	});
	// 	return;
	//   }
	//   // If the number of receivers is not the same size as connecting pipe's one
	//   if (nReceivers !== unestablishedPipe.nReceivers) {
	// 	res.writeHead(400, {
	// 	  "Access-Control-Allow-Origin": "*"
	// 	});
	// 	res.end(`[ERROR] The number of receivers should be ${unestablishedPipe.nReceivers} but ${nReceivers}.\n`);
	// 	return;
	//   }
	if len(unestablished.recievers) == unestablished.nRecievers {
		http.Error(w, "[ERROR] the number of recievers has reached limit", http.StatusBadRequest)
		return
	}
	//   // If more receivers can not connect
	//   if (unestablishedPipe.receivers.length === unestablishedPipe.nReceivers) {
	// 	res.writeHead(400, {
	// 	  "Access-Control-Allow-Origin": "*"
	// 	});
	// 	res.end("[ERROR] The number of receivers has reached limits.\n");
	// 	return;
	//   }
	reciever := s.createSenderOrReciever(recieverType, w, r)
	unestablished.recievers = append(unestablished.recievers, reciever)
	s.pathToUnestablishedPipe[path] = unestablished
	//   // Create a receiver
	//   const receiver = this.createSenderOrReceiver("receiver", req, res, reqPath);
	//   // Append new receiver
	//   unestablishedPipe.receivers.push(receiver);
	if !cmp.Equal(unestablished.sender, blankReqResAndUnsubscribe) {
		unestablished.sender.reqRes.res.Write([]byte("[INFO] a receiver has connected\n"))
	}
	//   if (unestablishedPipe.sender !== undefined) {
	// 	// Send connection message to the sender
	// 	unestablishedPipe.sender.reqRes.res.write("[INFO] A receiver was connected.\n");
	//   }
	p, err := unestablished.getPipeIfEstablished()
	if err != nil {
		s.log.Printf("checking if pipe is established: %s", err)
	}
	//   // Get pipeOpt if established
	//   const pipe: Pipe | undefined =
	// 	getPipeIfEstablished(unestablishedPipe);
	if !cmp.Equal(p, pipe{}) {
		s.runPipe(path, p)
	}
	//   if (pipe !== undefined) {
	// 	// Start data transfer
	// 	this.runPipe(reqPath, pipe);
	//   }
}

func (s *server) handleSender(w http.ResponseWriter, r *http.Request) {
	// // Get the number of receivers
	nRecievers := getNumberOfRecievers(r)
	if nRecievers <= 0 {
		s.log.Printf("[ERROR] url parameter 'n' should be greater than 0, but recieved: %d", nRecievers)
		http.Error(w, "[ERROR] specify a positive number of recievers", http.StatusBadRequest)
		return
	}
	// const nReceivers = Server.getNReceivers(req.url);
	// // If the number of receivers is invalid
	// if (nReceivers <= 0) {
	//   res.writeHead(400, {
	//     "Access-Control-Allow-Origin": "*"
	//   });
	//   res.end(`[ERROR] n should > 0, but n = ${nReceivers}.\n`);
	//   return;
	// }
	path := r.URL.Path
	if s.pathToEstablished.has(path) {
		http.Error(w, fmt.Sprintf("[ERROR] Connection on %s has been established already\n", path), http.StatusConflict)
		return
	}
	// if (this.pathToEstablished.has(reqPath)) {
	//   res.writeHead(400, {
	//     "Access-Control-Allow-Origin": "*"
	//   });
	//   res.end(`[ERROR] Connection on '${reqPath}' has been established already.\n`);
	//   return;
	// }
	unestablished, ok := s.pathToUnestablishedPipe[path]
	if !ok {
		sender := s.createSenderOrReciever(senderType, w, r)
		s.pathToUnestablishedPipe[path] = unestablishedPipe{
			recievers:  []reqResAndUnsubscribe{},
			sender:     sender,
			nRecievers: nRecievers,
		}
	}
	// // Get unestablished pipe
	// const unestablishedPipe = this.pathToUnestablishedPipe.get(reqPath);
	// // If the path connection is not connecting
	// if (unestablishedPipe === undefined) {
	//   // Add headers
	//   res.writeHead(200, {
	//     "Access-Control-Allow-Origin": "*"
	//   });
	//   // Send waiting message
	//   res.write(`[INFO] Waiting for ${nReceivers} receiver(s)...\n`);
	//   // Create a sender
	//   const sender = this.createSenderOrReceiver("sender", req, res, reqPath);
	//   // Register new unestablished pipe
	//   this.pathToUnestablishedPipe.set(reqPath, {
	//     sender: sender,
	//     receivers: [],
	//     nReceivers: nReceivers
	//   });
	//   return;
	// }
	if !cmp.Equal(unestablished.sender, blankReqResAndUnsubscribe) {
		http.Error(w, "[ERROR] another sender is already connected", http.StatusBadRequest)
		return
	}
	// // If a sender has been connected already
	// if (unestablishedPipe.sender !== undefined) {
	//   res.writeHead(400, {
	//     "Access-Control-Allow-Origin": "*"
	//   });
	//   res.end(`[ERROR] Another sender has been connected on '${reqPath}'.\n`);
	//   return;
	// }
	if unestablished.nRecievers != nRecievers {
		http.Error(w, "[ERROR] the number of recievers is incorrect", http.StatusBadRequest)
		return
	}
	// // If the number of receivers is not the same size as connecting pipe's one
	// if (nReceivers !== unestablishedPipe.nReceivers) {
	//   res.writeHead(400, {
	//     "Access-Control-Allow-Origin": "*"
	//   });
	//   res.end(`[ERROR] The number of receivers should be ${unestablishedPipe.nReceivers} but ${nReceivers}.\n`);
	//   return;
	// }
	unestablished.sender = s.createSenderOrReciever(senderType, w, r)
	// // Register the sender
	// unestablishedPipe.sender = this.createSenderOrReceiver("sender", req, res, reqPath);
	// // Add headers
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// res.writeHead(200, {
	//   "Access-Control-Allow-Origin": "*"
	// });
	// // Send waiting message
	w.Write([]byte(fmt.Sprintf("[INFO] Waiting for %d reciever(s)...\n", nRecievers)))
	// res.write(`[INFO] Waiting for ${nReceivers} receiver(s)...\n`);
	// // Send the number of receivers information
	w.Write([]byte(fmt.Sprintf("[INFO] %d reciever(s) have connected\n\n", len(unestablished.recievers))))
	// res.write(`[INFO] ${unestablishedPipe.receivers.length} receiver(s) has/have been connected.\n`);
	p, err := unestablished.getPipeIfEstablished()
	if err != nil {
		s.log.Printf("checking if pipe is established: %s", err)
	}
	//   // Get pipeOpt if established
	//   const pipe: Pipe | undefined =
	// 	getPipeIfEstablished(unestablishedPipe);
	if !cmp.Equal(p, pipe{}) {
		s.runPipe(path, p)
	}
	// if (pipe !== undefined) {
	//   // Start data transfer
	//   this.runPipe(reqPath, pipe);
	// }
}

func (s *server) runPipe(path string, pipe pipe) {
	//private async runPipe(path: string, pipe: Pipe): Promise<void> {
	// 	// Add to established
	// 	this.pathToEstablished.add(path);
	s.pathToEstablished.add(path)
	// 	// Delete unestablished pipe
	// 	this.pathToUnestablishedPipe.delete(path);
	delete(s.pathToUnestablishedPipe, path)
	// 	const {sender, receivers} = pipe;
	sender := pipe.sender
	recievers := pipe.recievers

	// 	// Emit message to sender
	// 	sender.res.write(`[INFO] Start sending to ${pipe.receivers.length} receiver(s)!\n`);
	sender.res.Write([]byte(fmt.Sprintf("[INFO] Start sending to %d reciever(s)!\n", len(recievers))))

	// 	this.params.logger?.info(`Sending: path='${path}', receivers=${pipe.receivers.length}`);
	s.log.Printf("Sending: path=%q, recievers=%d", path, len(recievers))

	// 	const isMultipart: boolean = (sender.req.headers["content-type"] ?? "").includes("multipart/form-data");
	isMultipart := false

	// 	const part: multiparty.Part | undefined =
	// 	  isMultipart ?
	// 		await new Promise((resolve, reject) => {
	// 		  const form = new multiparty.Form();
	// 		  form.once("part", (p: multiparty.Part) => {
	// 			resolve(p);
	// 		  });
	// 		  form.on("error", () => {
	// 			this.params.logger?.info(`sender-multipart on-error: '${path}'`);
	// 		  });
	// 		  // TODO: Not use any
	// 		  form.parse(sender.req as any);
	// 		}) :
	// 		undefined;

	// 	const senderData: stream.Readable =
	// 	  part === undefined ? sender.req : part;

	var firstMultipartPart *multipart.Part
	senderData := sender.req.Body
	contentType := sender.req.Header.Get("Content-Type")
	mediaType, mediaTypeParams, err := mime.ParseMediaType(contentType)
	if err != nil {
		s.log.Printf("Error parsing content-type: %s", err)
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(sender.req.Body, mediaTypeParams["boundary"])
		p, err := mr.NextPart()
		if err != nil && err != io.EOF {
			s.log.Printf("Error fetching next part of expected multipart body: %s", err)
		}
		if err == nil {
			isMultipart = true
			firstMultipartPart = p
			senderData = firstMultipartPart
		}
	}
	if isMultipart {
		contentType = firstMultipartPart.Header.Get("Content-Type")
		mediaType, mediaTypeParams, err = mime.ParseMediaType(contentType)
		if err != nil {
			s.log.Printf("Error parsing part content-type: %s", err)
		}
	}
	// If it is text/html, it should replace it with text/plain not to render in browser.
	// It is the same as GitHub Raw (https://raw.githubusercontent.com).
	// "text/plain" can be consider a superordinate concept of "text/html"
	if mediaType == "text/html" {
		contentType = strings.ReplaceAll(contentType, "text/html", "text/plain")
	}
	contentDisposition := sender.req.Header.Get("Content-Disposition")
	if isMultipart {
		contentDisposition = firstMultipartPart.Header.Get("Content-Disposition")
	}

	// 	let abortedCount: number = 0;
	// 	let endCount: number = 0;
	// 	for (const receiver of receivers) {
	// 	  // Close receiver
	// 	  const abortedListener = (): void => {
	// 		abortedCount++;
	// 		sender.res.write("[INFO] A receiver aborted.\n");
	// 		senderData.unpipe(passThrough);
	// 		// If aborted-count is # of receivers
	// 		if (abortedCount === receivers.length) {
	// 		  sender.res.end("[INFO] All receiver(s) was/were aborted halfway.\n");
	// 		  // Delete from established
	// 		  this.removeEstablished(path);
	// 		  // Close sender
	// 		  sender.req.destroy();
	// 		}
	// 	  };
	// 	  // End
	go func() {
		<-sender.req.Context().Done()
		s.log.Printf("sender went away")
		s.removeEstablished(path)
	}()

	abortedCount := 0
	endCount := 0
	for _, reciever := range recievers {
		recieverCtx, recieverCancel := context.WithCancel(sender.req.Context())
		abortedReciever := func() {
			abortedCount++
			sender.res.Write([]byte(fmt.Sprintf("[INFO] A reciever aborted\n")))
			recieverCancel()
			if abortedCount == len(recievers) {
				sender.res.Write([]byte(fmt.Sprintf("[INFO] All recievers aborted part way\n")))
				s.removeEstablished(path)
				return
			}
		}
		// 	  const endListener = (): void => {
		// 		endCount++;
		// 		// If end-count is # of receivers
		// 		if (endCount === receivers.length) {
		// 		  sender.res.end("[INFO] All receiver(s) was/were received successfully.\n");
		// 		  // Delete from established
		// 		  this.removeEstablished(path);
		// 		}
		// 	  };
		completeReciever := func() {
			endCount++
			if endCount == len(recievers) {
				sender.res.Write([]byte(fmt.Sprintf("[INFO] All recievers completed successfully!\n")))
				s.removeEstablished(path)
				return
			}
		}

		// 	  // Decide Content-Length
		// 	  const contentLength: string | number | undefined = part === undefined ?
		// 		sender.req.headers["content-length"] : part.byteCount;
		// 	  // Get Content-Type from part or HTTP header.
		// 	  const contentType: string | undefined = (() => {
		// 		const type: string | undefined = (part === undefined ?
		// 		  sender.req.headers["content-type"] : part.headers["content-type"]);
		// 		if (type === undefined) {
		// 		  return undefined;
		// 		}
		// 		const matched = type.match(/^\s*([^;]*)(\s*;?.*)$/);
		// 		// If invalid Content-Type
		// 		if (matched === null) {
		// 		  return undefined;
		// 		} else {
		// 		  // Extract MIME type and parameters
		// 		  const mimeType: string = matched[1];
		// 		  const params: string = matched[2];
		// 		  // If it is text/html, it should replace it with text/plain not to render in browser.
		// 		  // It is the same as GitHub Raw (https://raw.githubusercontent.com).
		// 		  // "text/plain" can be consider a superordinate concept of "text/html"
		// 		  return mimeType === "text/html" ? "text/plain" + params : type;
		// 		}
		// 	  })();
		// 	  const contentDisposition: string | undefined = part === undefined ?
		// 		sender.req.headers["content-disposition"] : part.headers["content-disposition"];

		// 	  // Write headers to a receiver
		// 	  receiver.res.writeHead(200, {
		// 		...(contentLength === undefined ? {} : {"Content-Length": contentLength}),
		// 		...(contentType === undefined ? {} : {"Content-Type": contentType}),
		// 		...(contentDisposition === undefined ? {} : {"Content-Disposition": contentDisposition}),
		// 		"Access-Control-Allow-Origin": "*",
		// 		"Access-Control-Expose-Headers": "Content-Length, Content-Type",
		// 		"X-Content-Type-Options": "nosniff"
		// 	  });
		reciever.res.Header().Add("Content-Type", contentType)
		reciever.res.Header().Add("Content-Disposition", contentDisposition)
		reciever.res.Header().Add("Access-Control-Allow-Origin", "*")
		reciever.res.Header().Add("Access-Control-Expose-Headers", "Content-Length, Content-Type")
		reciever.res.Header().Add("X-Content-Type-Options", "nosniff")

		// 	  const passThrough = new stream.PassThrough();
		// 	  senderData.pipe(passThrough);
		// 	  // TODO: Not use any
		// 	  passThrough.pipe(receiver.res as any);
		// 	  receiver.req.on("end", () => {
		// 		this.params.logger?.info(`receiver on-end: '${path}'`);
		// 		endListener();
		// 	  });
		// 	  receiver.req.on("close", () => {
		// 		this.params.logger?.info(`receiver on-close: '${path}'`);
		// 	  });
		// 	  receiver.req.on("aborted", () => {
		// 		this.params.logger?.info(`receiver on-aborted: '${path}'`);
		// 		abortedListener();
		// 	  });
		// 	  receiver.req.on("error", (err) => {
		// 		this.params.logger?.info(`receiver on-error: '${path}'`);
		// 		abortedListener();
		// 	  });
		// 	}

		go func() {
			<-reciever.req.Context().Done()
			s.log.Printf("sender went away")
			abortedReciever()
		}()

		_, err = io.Copy(reciever.res, contextio.NewReader(recieverCtx, senderData))
		if err != nil {
			s.log.Printf("io.Copy interrupted: %s", err)
			sender.res.Write([]byte(fmt.Sprintf("[ERROR] issue sending to a reciever: %s", err)))
		}
		s.log.Printf("reciever complete: %s", path)
		completeReciever()
	}

	// 	senderData.on("close", () => {
	// 	  this.params.logger?.info(`sender on-close: '${path}'`);
	// 	});

	// 	senderData.on("aborted", () => {
	// 	  for (const receiver of receivers) {
	// 		// Close a receiver
	// 		if (receiver.res.connection !== undefined) {
	// 		  receiver.res.connection.destroy();
	// 		}
	// 	  }
	// 	  this.params.logger?.info(`sender on-aborted: '${path}'`);
	// 	});

	// 	senderData.on("end", () => {
	// 	  sender.res.write("[INFO] Sent successfully!\n");
	// 	  this.params.logger?.info(`sender on-end: '${path}'`);
	// 	});

	// 	senderData.on("error", (error) => {
	// 	  sender.res.end("[ERROR] Failed to send.\n");
	// 	  // Delete from established
	// 	  this.removeEstablished(path);
	// 	  this.params.logger?.info(`sender on-error: '${path}'`);
	// 	});
	//}
}

func (s *server) removeEstablished(path string) {
	s.pathToEstablished.del(path)
	delete(s.pathToUnestablishedPipe, path)
}

type removerType string

var (
	senderType   removerType = "sender"
	recieverType removerType = "reciever"
)

func (s *server) createSenderOrReciever(removerType removerType, res http.ResponseWriter, req *http.Request) reqResAndUnsubscribe {
	// private createSenderOrReceiver(
	// 	removerType: "sender" | "receiver",
	// 	req: HttpReq,
	// 	res: HttpRes,
	// 	reqPath: string
	//   ): ReqResAndUnsubscribe {
	// 	// Create receiver req&res
	// 	const receiverReqRes: ReqRes = { req: req, res: res };

	receiverReqRes := reqRes{req: req, res: res}
	closeListener := func() {
		path := req.URL.Path
		unestablishedPipe, ok := s.pathToUnestablishedPipe[path]
		if ok {
			var remover func() bool
			switch removerType {
			case senderType:
				remover = func() bool {
					if !cmp.Equal(unestablishedPipe.sender, blankReqResAndUnsubscribe) {
						unestablishedPipe.sender = blankReqResAndUnsubscribe
						s.pathToUnestablishedPipe[path] = unestablishedPipe
						return true
					}
					return false
				}
			case recieverType:
				remover = func() bool {
					found := false
					recievers := []reqResAndUnsubscribe{}
					for _, reciever := range unestablishedPipe.recievers {
						if cmp.Equal(reciever, unestablishedPipe) {
							found = true
							continue
						}
						recievers = append(recievers, reciever)
					}
					unestablishedPipe.recievers = recievers
					s.pathToUnestablishedPipe[path] = unestablishedPipe
					return found
				}
			}
			removed := remover()
			if removed {
				if len(unestablishedPipe.recievers) == 0 && cmp.Equal(unestablishedPipe.sender, blankReqResAndUnsubscribe) {
					delete(s.pathToUnestablishedPipe, path)
					s.log.Printf("Unestablished path %s removed", path)
				}
			}
		}
	}
	var unsubscribeCloseListener = func() {
		s.log.Printf("unsubscribeCloseListener starting")
		<-req.Context().Done()
		s.log.Printf("someone went away")
		closeListener()
	}

	// 	// Define on-close handler
	// 	const closeListener = () => {
	// 	  // Get unestablished pipe
	// 	  const unestablishedPipe = this.pathToUnestablishedPipe.get(reqPath);
	// 	  // If the pipe is registered
	// 	  if (unestablishedPipe !== undefined) {
	// 		// Get sender/receiver remover
	// 		const remover =
	// 		  removerType === "sender" ?
	// 			(): boolean => {
	// 			  // If sender is defined
	// 			  if (unestablishedPipe.sender !== undefined) {
	// 				// Remove sender
	// 				unestablishedPipe.sender = undefined;
	// 				return true;
	// 			  }
	// 			  return false;
	// 			} :
	// 			(): boolean => {
	// 			  // Get receivers
	// 			  const receivers = unestablishedPipe.receivers;
	// 			  // Find receiver's index
	// 			  const idx = receivers.findIndex((r) => r.reqRes === receiverReqRes);
	// 			  // If receiver is found
	// 			  if (idx !== -1) {
	// 				// Delete the receiver from the receivers
	// 				receivers.splice(idx, 1);
	// 				return true;
	// 			  }
	// 			  return false;
	// 			};
	// 		// Remove a sender or receiver
	// 		const removed: boolean = remover();
	// 		// If removed
	// 		if (removed) {
	// 		  // If unestablished pipe has no sender and no receivers
	// 		  if (unestablishedPipe.receivers.length === 0 && unestablishedPipe.sender === undefined) {
	// 			// Remove unestablished pipe
	// 			this.pathToUnestablishedPipe.delete(reqPath);
	// 			this.params.logger?.info(`unestablished '${reqPath}' removed`);
	// 		  }
	// 		}
	// 	  }
	// 	};
	// 	// Disconnect if it close
	// 	req.once("close", closeListener);
	// 	// Unsubscribe "close"
	// 	const unsubscribeCloseListener = () => {
	// 	  req.removeListener("close", closeListener);
	// 	};
	// 	return {
	// 	  reqRes: receiverReqRes,
	// 	  unsubscribeCloseListener: unsubscribeCloseListener
	// 	};
	//   }
	return reqResAndUnsubscribe{reqRes: receiverReqRes, unsubscribeCloseListener: unsubscribeCloseListener}
}
