package main

import (
    "fmt"
    "time"
    "github.com/titos-carrasco/MindWave-Go"
)

func main() {
    // colocar el headset unos 4 minutos antes para que se estabilice
    // el Global Headset Unique Identifier está en la zona de la batería
    mw := mindwave.NewMindWave( "/dev/ttyUSB0", 0xF6, 0x4F )
    err := mw.Connect()
    if( err == nil ) {
        for i:=0; i<=30000; i=i+1 {
            fmt.Println( mw.GlobalHeadsetID(), mw.MindWaveData() )

            // requerido para el scheduler
            time.Sleep( time.Millisecond * 1 )
        }
        mw.Disconnect()
    }
}

